data "conohavps_backups" "server" {
  instance_id = "11111111-1111-1111-1111-111111111111" # Replace with your server ID
}

# Restore the newest backup to a new volume
resource "conohavps_volume" "restored" {
  name        = "example-restored-volume"
  size        = data.conohavps_backups.server.backups[0].size
  volume_type = "c3j1-ds02-boot"
  backup_id   = data.conohavps_backups.server.backups[0].id
}
