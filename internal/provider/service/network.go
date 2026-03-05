// ネットワーク関連のAPIの呼び出しを提供する.

package service

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// GetPortsByDeviceID デバイスIDに紐づくポートを取得.
func (c *ConohaClient) GetPortsByDeviceID(ctx context.Context, deviceID string) ([]ports.Port, error) {
	if c.NetworkClient == nil {
		return nil, fmt.Errorf("network client is not initialized")
	}

	tflog.Debug(ctx, "Retrieving port list.", map[string]any{
		"device_id": deviceID,
	})

	listOpts := ports.ListOpts{
		DeviceID: deviceID,
	}

	allPages, err := ports.List(c.NetworkClient, listOpts).AllPages(ctx)
	if err != nil {
		tflog.Error(ctx, "Port list retrieval error.", map[string]any{
			"error":     err.Error(),
			"device_id": deviceID,
		})
		return nil, fmt.Errorf("failed to retrieve port list: %w", err)
	}

	portList, err := ports.ExtractPorts(allPages)
	if err != nil {
		tflog.Error(ctx, "Port list parsing error.", map[string]any{
			"error":     err.Error(),
			"device_id": deviceID,
		})
		return nil, fmt.Errorf("failed to parse port list: %w", err)
	}

	tflog.Debug(ctx, "Port list retrieved successfully.", map[string]any{
		"device_id":  deviceID,
		"port_count": len(portList),
	})

	return portList, nil
}

// SecurityGroupUpdate セキュリティグループ更新用の構造体.
type SecurityGroupUpdate struct {
	Name string `json:"name"`
}

// GetSecurityGroupIDByName セキュリティグループ名からIDを取得.
func (c *ConohaClient) GetSecurityGroupIDByName(ctx context.Context, name string) (string, error) {
	if c.NetworkClient == nil {
		return "", fmt.Errorf("network client is not initialized")
	}

	tflog.Debug(ctx, "Retrieving security group by name.", map[string]any{
		"name": name,
	})

	listOpts := groups.ListOpts{
		Name: name,
	}

	allPages, err := groups.List(c.NetworkClient, listOpts).AllPages(ctx)
	if err != nil {
		tflog.Error(ctx, "Security group list retrieval error.", map[string]any{
			"error": err.Error(),
			"name":  name,
		})
		return "", fmt.Errorf("failed to retrieve security group list: %w", err)
	}

	sgList, err := groups.ExtractGroups(allPages)
	if err != nil {
		tflog.Error(ctx, "Security group list parsing error.", map[string]any{
			"error": err.Error(),
			"name":  name,
		})
		return "", fmt.Errorf("failed to parse security group list: %w", err)
	}

	if len(sgList) == 0 {
		return "", fmt.Errorf("security group not found: %s", name)
	}

	if len(sgList) > 1 {
		tflog.Warn(ctx, "Multiple security groups found with same name, using first one", map[string]any{
			"name":  name,
			"count": len(sgList),
		})
	}

	tflog.Debug(ctx, "Security group found.", map[string]any{
		"name": name,
		"id":   sgList[0].ID,
	})

	return sgList[0].ID, nil
}

// UpdatePortSecurityGroup ポートのセキュリティグループを更新.
func (c *ConohaClient) UpdatePortSecurityGroup(ctx context.Context, portID string, securityGroups []SecurityGroupUpdate) error {
	if c.NetworkClient == nil {
		return fmt.Errorf("network client is not initialized")
	}

	tflog.Debug(ctx, "Updating port security groups.", map[string]any{
		"port_id":         portID,
		"security_groups": securityGroups,
	})

	// セキュリティグループ名からIDに変換
	sgIDs := make([]string, 0, len(securityGroups))
	for _, sg := range securityGroups {
		sgID, err := c.GetSecurityGroupIDByName(ctx, sg.Name)
		if err != nil {
			tflog.Error(ctx, "Failed to get security group ID.", map[string]any{
				"name":  sg.Name,
				"error": err.Error(),
			})
			return fmt.Errorf("failed to get security group ID for '%s': %w", sg.Name, err)
		}
		sgIDs = append(sgIDs, sgID)
	}

	tflog.Debug(ctx, "Security group IDs resolved.", map[string]any{
		"port_id": portID,
		"sg_ids":  sgIDs,
	})

	updateOpts := ports.UpdateOpts{
		SecurityGroups: &sgIDs,
	}

	result := ports.Update(ctx, c.NetworkClient, portID, updateOpts)
	if result.Err != nil {
		tflog.Error(ctx, "Port security group update error.", map[string]any{
			"error":   result.Err.Error(),
			"port_id": portID,
		})
		return fmt.Errorf("failed to update port security groups: %w", result.Err)
	}

	tflog.Debug(ctx, "Port security groups updated successfully.", map[string]any{
		"port_id": portID,
		"sg_ids":  sgIDs,
	})

	return nil
}

// UpdateInstanceSecurityGroups インスタンスに紐づくext-portのセキュリティグループを更新.
// ext-ポートは1つのみという仕様.
func (c *ConohaClient) UpdateInstanceSecurityGroups(ctx context.Context, instanceID string, securityGroups []SecurityGroupUpdate) error {
	tflog.Debug(ctx, "Updating instance security groups.", map[string]any{
		"instance_id":     instanceID,
		"security_groups": securityGroups,
	})

	portList, err := c.GetPortsByDeviceID(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to retrieve ports: %w", err)
	}

	// ext-で始まるポートを検索
	var extPort *ports.Port
	for i, port := range portList {
		if port.Name != "" && len(port.Name) >= 4 && port.Name[:4] == "ext-" {
			extPort = &portList[i]
			break
		}
	}

	if extPort == nil {
		tflog.Warn(ctx, "ext- port not found", map[string]any{
			"instance_id": instanceID,
		})
		return nil
	}

	tflog.Debug(ctx, "ext- port detected.", map[string]any{
		"instance_id": instanceID,
		"port_id":     extPort.ID,
		"port_name":   extPort.Name,
	})

	err = c.UpdatePortSecurityGroup(ctx, extPort.ID, securityGroups)
	if err != nil {
		tflog.Error(ctx, "Port update failed.", map[string]any{
			"port_id":     extPort.ID,
			"instance_id": instanceID,
			"error":       err.Error(),
		})
		return fmt.Errorf("failed to update port %s: %w", extPort.ID, err)
	}

	tflog.Debug(ctx, "Instance security groups updated successfully.", map[string]any{
		"instance_id":     instanceID,
		"port_id":         extPort.ID,
		"security_groups": securityGroups,
	})

	return nil
}
