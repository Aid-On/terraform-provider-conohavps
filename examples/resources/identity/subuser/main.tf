# Give an AI agent its own API user that can only issue a token and operate servers,
# instead of handing it the account's main API user.

variable "agent_password" {
  type      = string
  sensitive = true
}

resource "conohavps_role" "agent" {
  name = "ai-agent"
  permissions = [
    "get-server-list",
    "get-server",
    "post-server-action-start",
    "post-server-action-stop",
    "post-server-action-reboot",
  ]
}

resource "conohavps_subuser" "agent" {
  password = var.agent_password
  roles = [
    "gmo-identity",          # standard role that allows issuing a token
    conohavps_role.agent.id, # the narrowed role above
  ]
}

# The agent authenticates with this user ID, the password and the account's tenant ID
output "agent_user_id" {
  value = conohavps_subuser.agent.id
}
