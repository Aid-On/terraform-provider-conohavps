# A role that can only look at servers and reboot them
resource "conohavps_role" "server_operator" {
  name = "server-operator"
  permissions = [
    "get-server-list",
    "get-server",
    "post-server-action-reboot",
  ]
}
