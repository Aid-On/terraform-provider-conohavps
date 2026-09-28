// データソースを提供する.

package datasource

import (
	"fmt"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

// プロバイダから渡された API クライアントを取り出す.
func clientOf(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *service.ConohaClient {
	if req.ProviderData == nil {
		return nil
	}

	conohaClient, ok := req.ProviderData.(*service.ConohaClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected data source configure type",
			fmt.Sprintf("Expected *service.ConohaClient, but got: %T.", req.ProviderData),
		)
		return nil
	}

	return conohaClient
}
