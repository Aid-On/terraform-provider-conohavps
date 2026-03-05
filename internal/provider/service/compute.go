// コンピュート関連のAPIの呼び出しを提供する.

package service

import (
	"context"
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// CreateKeypair キーペアを作成.
func (c *ConohaClient) CreateKeypair(ctx context.Context, name string, opts map[string]interface{}) (*keypairs.KeyPair, error) {
	tflog.Debug(ctx, "Creating keypair.", map[string]any{
		"name": name,
		"opts": opts,
	})

	if c.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	// キーペア作成オプションを構築
	createOpts := keypairs.CreateOpts{
		Name: name,
	}

	if publicKey, ok := opts["public_key"]; ok {
		if pk, ok := publicKey.(string); ok {
			createOpts.PublicKey = pk
		}
	}

	// キーペア作成
	result := keypairs.Create(ctx, c.ComputeClient, createOpts)
	if result.Err != nil {
		tflog.Error(ctx, "Keypair creation error.", map[string]any{
			"error": result.Err.Error(),
			"name":  name,
		})
		return nil, fmt.Errorf("failed to create keypair: %w", result.Err)
	}

	keypair, err := result.Extract()
	if err != nil {
		tflog.Error(ctx, "Response parsing error.", map[string]any{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	tflog.Debug(ctx, "Keypair created successfully.", map[string]any{
		"name":       keypair.Name,
		"public_key": len(keypair.PublicKey) > 0,
	})

	return keypair, nil
}

// GetKeypair キーペア情報を取得.
func (c *ConohaClient) GetKeypair(ctx context.Context, keypairName string) (*keypairs.KeyPair, error) {
	if c.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Retrieving keypair.", map[string]any{
		"keypair_name": keypairName,
	})

	result := keypairs.Get(ctx, c.ComputeClient, keypairName, nil)
	if result.Err != nil {
		tflog.Error(ctx, "Keypair retrieval error.", map[string]any{
			"error":        result.Err.Error(),
			"keypair_name": keypairName,
		})
		return nil, fmt.Errorf("failed to retrieve keypair: %w", result.Err)
	}

	keypair, err := result.Extract()
	if err != nil {
		tflog.Error(ctx, "Response parsing error.", map[string]any{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return keypair, nil
}

// DeleteKeypair キーペアを削除.
func (c *ConohaClient) DeleteKeypair(ctx context.Context, keypairName string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Deleting keypair.", map[string]any{
		"keypair_name": keypairName,
	})

	result := keypairs.Delete(ctx, c.ComputeClient, keypairName, nil)
	if result.Err != nil {
		tflog.Error(ctx, "Keypair deletion error.", map[string]any{
			"error":        result.Err.Error(),
			"keypair_name": keypairName,
		})
		return fmt.Errorf("failed to delete keypair: %w", result.Err)
	}

	tflog.Debug(ctx, "Keypair deleted successfully.", map[string]any{
		"keypair_name": keypairName,
	})

	return nil
}

// CreateInstance インスタンスを作成.
func (c *ConohaClient) CreateInstance(ctx context.Context, name string, flavorRef string, blockDeviceMappingV2 []servers.BlockDevice, opts map[string]interface{}) (*servers.Server, error) {
	tflog.Debug(ctx, "Creating instance.", map[string]any{
		"name":         name,
		"flavor_ref":   flavorRef,
		"block_device": len(blockDeviceMappingV2),
		"opts":         opts,
	})

	if c.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	// インスタンス作成オプションを構築
	createOpts := &servers.CreateOpts{
		Name:      name,
		FlavorRef: flavorRef,
	}

	if len(blockDeviceMappingV2) > 0 {
		createOpts.BlockDevice = blockDeviceMappingV2
	}

	if adminPass, ok := opts["admin_pass"].(string); ok && adminPass != "" {
		createOpts.AdminPass = adminPass
	}

	if keyName, ok := opts["key_name"].(string); ok && keyName != "" {
		createOpts.KeyName = keyName
	}

	if securityGroups, ok := opts["security_groups"].([]string); ok && len(securityGroups) > 0 {
		createOpts.SecurityGroups = securityGroups
	}

	if metadata, ok := opts["metadata"].(map[string]string); ok && metadata != nil {
		createOpts.Metadata = metadata
	}

	if userData, ok := opts["user_data"].(string); ok && userData != "" {
		createOpts.UserData = []byte(userData)
	}

	// インスタンス作成
	result := servers.Create(ctx, c.ComputeClient, createOpts, nil)
	if result.Err != nil {
		tflog.Error(ctx, "Instance creation error.", map[string]any{
			"error": result.Err.Error(),
			"name":  name,
		})
		return nil, fmt.Errorf("failed to create instance: %w", result.Err)
	}

	instance, err := result.Extract()
	if err != nil {
		tflog.Error(ctx, "Response parsing error.", map[string]any{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	tflog.Debug(ctx, "Instance created successfully.", map[string]any{
		"id":     instance.ID,
		"status": instance.Status,
	})

	return instance, nil
}

// GetInstance インスタンス情報を取得.
func (c *ConohaClient) GetInstance(ctx context.Context, instanceID string) (*servers.Server, error) {
	if c.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Retrieving instance.", map[string]any{
		"instance_id": instanceID,
	})

	result := servers.Get(ctx, c.ComputeClient, instanceID)
	if result.Err != nil {
		tflog.Error(ctx, "Instance retrieval error.", map[string]any{
			"error":       result.Err.Error(),
			"instance_id": instanceID,
		})
		return nil, fmt.Errorf("failed to retrieve instance: %w", result.Err)
	}

	instance, err := result.Extract()
	if err != nil {
		tflog.Error(ctx, "Response parsing error.", map[string]any{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return instance, nil
}

// DeleteInstance インスタンスを削除.
func (c *ConohaClient) DeleteInstance(ctx context.Context, instanceID string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Deleting instance.", map[string]any{
		"instance_id": instanceID,
	})

	result := servers.Delete(ctx, c.ComputeClient, instanceID)
	if result.Err != nil {
		tflog.Error(ctx, "Instance deletion error.", map[string]any{
			"error":       result.Err.Error(),
			"instance_id": instanceID,
		})
		return fmt.Errorf("failed to delete instance: %w", result.Err)
	}

	tflog.Debug(ctx, "Instance deleted successfully.", map[string]any{
		"instance_id": instanceID,
	})

	return nil
}

// WaitForInstanceState インスタンスの状態を待機.
func (c *ConohaClient) WaitForInstanceState(ctx context.Context, instanceID string, targetStatus string, timeout time.Duration) (*servers.Server, error) {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout: waiting for instance to become %s (timeout: %v)", targetStatus, timeout)
		}

		instance, err := c.GetInstance(ctx, instanceID)
		if err != nil {
			tflog.Warn(ctx, "Instance state check error.", map[string]any{
				"error": err.Error(),
			})
			time.Sleep(2 * time.Second)
			continue
		}

		tflog.Debug(ctx, "Checking instance state.", map[string]any{
			"current_status": instance.Status,
			"target_status":  targetStatus,
		})

		if instance.Status == targetStatus {
			return instance, nil
		}

		time.Sleep(2 * time.Second)
	}
}

// StartInstance インスタンスを起動.
func (c *ConohaClient) StartInstance(ctx context.Context, instanceID string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Starting instance.", map[string]any{
		"instance_id": instanceID,
	})

	result := servers.Start(ctx, c.ComputeClient, instanceID)
	if result.Err != nil {
		tflog.Error(ctx, "Instance start error.", map[string]any{
			"error": result.Err.Error(),
		})
		return fmt.Errorf("failed to start instance: %w", result.Err)
	}

	tflog.Debug(ctx, "Instance started successfully.", map[string]any{
		"instance_id": instanceID,
	})

	return nil
}

// StopInstance インスタンスを停止.
func (c *ConohaClient) StopInstance(ctx context.Context, instanceID string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Stopping instance.", map[string]any{
		"instance_id": instanceID,
	})

	result := servers.Stop(ctx, c.ComputeClient, instanceID)
	if result.Err != nil {
		tflog.Error(ctx, "Instance stop error.", map[string]any{
			"error": result.Err.Error(),
		})
		return fmt.Errorf("failed to stop instance: %w", result.Err)
	}

	tflog.Debug(ctx, "Instance stopped successfully.", map[string]any{
		"instance_id": instanceID,
	})

	return nil
}

// ResizeInstance インスタンスをリサイズ（プラン変更）.
func (c *ConohaClient) ResizeInstance(ctx context.Context, instanceID string, flavorRef string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Resizing instance.", map[string]any{
		"instance_id": instanceID,
		"flavor_ref":  flavorRef,
	})

	opts := servers.ResizeOpts{
		FlavorRef: flavorRef,
	}

	result := servers.Resize(ctx, c.ComputeClient, instanceID, opts)
	if result.Err != nil {
		tflog.Error(ctx, "Instance resize error.", map[string]any{
			"error": result.Err.Error(),
		})
		return fmt.Errorf("failed to resize instance: %w", result.Err)
	}

	tflog.Debug(ctx, "Instance resized successfully.", map[string]any{
		"instance_id": instanceID,
		"flavor_ref":  flavorRef,
	})

	return nil
}

// ConfirmResizeInstance リサイズを確定.
func (c *ConohaClient) ConfirmResizeInstance(ctx context.Context, instanceID string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Confirming resize.", map[string]any{
		"instance_id": instanceID,
	})

	result := servers.ConfirmResize(ctx, c.ComputeClient, instanceID)
	if result.Err != nil {
		tflog.Error(ctx, "Resize confirmation error.", map[string]any{
			"error": result.Err.Error(),
		})
		return fmt.Errorf("failed to confirm resize: %w", result.Err)
	}

	tflog.Debug(ctx, "Resize confirmed successfully.", map[string]any{
		"instance_id": instanceID,
	})

	return nil
}

// RevertResizeInstance リサイズをキャンセル.
func (c *ConohaClient) RevertResizeInstance(ctx context.Context, instanceID string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Reverting resize.", map[string]any{
		"instance_id": instanceID,
	})

	result := servers.RevertResize(ctx, c.ComputeClient, instanceID)
	if result.Err != nil {
		tflog.Error(ctx, "Resize revert error.", map[string]any{
			"error": result.Err.Error(),
		})
		return fmt.Errorf("failed to revert resize: %w", result.Err)
	}

	tflog.Debug(ctx, "Resize reverted successfully.", map[string]any{
		"instance_id": instanceID,
	})

	return nil
}

// UpdateInstanceMetadata インスタンスのメタデータを更新.
func (c *ConohaClient) UpdateInstanceMetadata(ctx context.Context, instanceID string, metadata map[string]string) error {
	if c.ComputeClient == nil {
		return fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Updating instance metadata.", map[string]any{
		"instance_id": instanceID,
	})

	opts := servers.MetadataOpts(metadata)

	result := servers.UpdateMetadata(ctx, c.ComputeClient, instanceID, opts)
	if result.Err != nil {
		tflog.Error(ctx, "Metadata update error.", map[string]any{
			"error": result.Err.Error(),
		})
		return fmt.Errorf("failed to update metadata: %w", result.Err)
	}

	tflog.Debug(ctx, "Metadata updated successfully.", map[string]any{
		"instance_id": instanceID,
	})

	return nil
}
