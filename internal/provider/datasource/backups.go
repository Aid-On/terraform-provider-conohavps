// 自動バックアップで取得されたバックアップの一覧のデータソースを提供する.
// サーバー ID やボリューム ID で絞り込み、作成日時の新しい順に返す.

package datasource

import (
	"context"
	"sort"
	"strings"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &backupsDataSource{}

func NewBackupsDataSource() datasource.DataSource {
	return &backupsDataSource{}
}

type backupsDataSource struct {
	client *service.ConohaClient
}

type backupsDataSourceModel struct {
	InstanceID types.String  `tfsdk:"instance_id"` // 絞り込むサーバー ID
	VolumeID   types.String  `tfsdk:"volume_id"`   // 絞り込むボリューム ID
	Backups    []backupModel `tfsdk:"backups"`     // バックアップ
}

type backupModel struct {
	ID            types.String `tfsdk:"id"`             // バックアップ ID
	Name          types.String `tfsdk:"name"`           // バックアップ名
	Status        types.String `tfsdk:"status"`         // ステータス
	Size          types.Int64  `tfsdk:"size"`           // サイズ（GB）
	VolumeID      types.String `tfsdk:"volume_id"`      // バックアップ元のボリューム ID
	InstanceID    types.String `tfsdk:"instance_id"`    // バックアップ元のサーバー ID
	IsBootVolume  types.Bool   `tfsdk:"is_boot_volume"` // ブートストレージのバックアップか
	CreatedAt     types.String `tfsdk:"created_at"`     // 作成日時
	DataTimestamp types.String `tfsdk:"data_timestamp"` // データの取得日時
}

func (d *backupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backups"
}

func (d *backupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the backups taken by auto-backup, newest first. Use a backup's `id` as `backup_id` of `conohavps_volume` to restore it.",
		Attributes: map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{
				MarkdownDescription: "Only list the backups of this server.",
				Optional:            true,
			},
			"volume_id": schema.StringAttribute{
				MarkdownDescription: "Only list the backups of this volume.",
				Optional:            true,
			},
			"backups": schema.ListNestedAttribute{
				MarkdownDescription: "The backups, newest first.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{MarkdownDescription: "Backup ID.", Computed: true},
						"name":           schema.StringAttribute{MarkdownDescription: "Backup name.", Computed: true},
						"status":         schema.StringAttribute{MarkdownDescription: "Backup status.", Computed: true},
						"size":           schema.Int64Attribute{MarkdownDescription: "Backup size in GB.", Computed: true},
						"volume_id":      schema.StringAttribute{MarkdownDescription: "The ID of the backed up volume.", Computed: true},
						"instance_id":    schema.StringAttribute{MarkdownDescription: "The ID of the server the volume was attached to.", Computed: true},
						"is_boot_volume": schema.BoolAttribute{MarkdownDescription: "Whether the backed up volume is a boot storage volume.", Computed: true},
						"created_at":     schema.StringAttribute{MarkdownDescription: "The date and time the backup was created.", Computed: true},
						"data_timestamp": schema.StringAttribute{MarkdownDescription: "The date and time of the backed up data.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *backupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientOf(req, resp)
}

func (d *backupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data backupsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	list, err := d.client.ListBackups(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list backups", err.Error())
		return
	}

	data.Backups = filterBackups(list, data.InstanceID.ValueString(), data.VolumeID.ValueString())

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// サーバー ID とボリューム ID で絞り込み（空なら絞り込まない）、作成日時の新しい順に並べる.
func filterBackups(list []service.Backup, instanceID, volumeID string) []backupModel {
	var found []service.Backup
	for _, b := range list {
		if instanceID != "" && b.Metadata["instance_uuid"] != instanceID {
			continue
		}
		if volumeID != "" && b.VolumeID != volumeID {
			continue
		}
		found = append(found, b)
	}

	// 作成日時は同じ書式の文字列なので、文字列の比較で並べられる
	sort.SliceStable(found, func(i, j int) bool { return found[i].CreatedAt > found[j].CreatedAt })

	backups := make([]backupModel, 0, len(found))
	for _, b := range found {
		backups = append(backups, backupModel{
			ID:            types.StringValue(b.ID),
			Name:          types.StringValue(b.Name),
			Status:        types.StringValue(b.Status),
			Size:          types.Int64Value(int64(b.Size)),
			VolumeID:      types.StringValue(b.VolumeID),
			InstanceID:    types.StringValue(b.Metadata["instance_uuid"]),
			IsBootVolume:  types.BoolValue(strings.EqualFold(b.Metadata["is_boot_volume"], "true")),
			CreatedAt:     types.StringValue(b.CreatedAt),
			DataTimestamp: types.StringValue(b.DataTimestamp),
		})
	}
	return backups
}
