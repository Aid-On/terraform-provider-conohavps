# An access key and secret key for S3-compatible clients of the object storage
resource "conohavps_credential" "backup" {
  user_id = var.api_user_id # the API user that owns the credential
}

variable "api_user_id" {
  type = string
}

output "access_key" {
  value = conohavps_credential.backup.access
}

output "secret_key" {
  value     = conohavps_credential.backup.secret
  sensitive = true
}
