resource "conohavps_volume" "data" {
  name        = "example-data-volume"
  size        = 200
  volume_type = "c3j1-ds02-add"
}

# The server must be stopped when the volume is attached and detached
resource "conohavps_volume_attachment" "data" {
  instance_id = "11111111-1111-1111-1111-111111111111" # Replace with your server ID
  volume_id   = conohavps_volume.data.id
}
