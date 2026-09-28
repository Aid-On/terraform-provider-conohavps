# Back up the volumes attached to the server every day and keep backups for 30 days
resource "conohavps_instance_autobackup" "example" {
  instance_id = "11111111-1111-1111-1111-111111111111" # Replace with your server ID
  retention   = 30
}
