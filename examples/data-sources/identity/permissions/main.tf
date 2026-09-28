# List the permissions that can be granted by a role
data "conohavps_permissions" "all" {}

# Grant every read-only (get-*) permission of the servers
resource "conohavps_role" "server_viewer" {
  name        = "server-viewer"
  permissions = [for name in data.conohavps_permissions.all.names : name if startswith(name, "get-server")]
}
