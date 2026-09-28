// 課金される容量（契約容量）の刻みを検証するバリデーターを提供する.

package resource

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// quotaStepValidator は、値が min 以上で、base から step 単位で増えた値であることを検証する.
// 例: オブジェクトストレージは min=100, base=0, step=100、イメージ保存容量は min=50, base=50, step=500.
type quotaStepValidator struct {
	min, base, step int64
}

func (v quotaStepValidator) Description(_ context.Context) string {
	if v.base == 0 {
		return fmt.Sprintf("value must be a multiple of %d and at least %d", v.step, v.min)
	}
	return fmt.Sprintf("value must be %d plus a multiple of %d (at least %d)", v.base, v.step, v.min)
}

func (v quotaStepValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v quotaStepValidator) ValidateInt64(ctx context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueInt64()
	if val >= v.min && (val-v.base)%v.step == 0 {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid attribute value",
		fmt.Sprintf("Attribute %s, got: %d.", v.Description(ctx), val),
	)
}
