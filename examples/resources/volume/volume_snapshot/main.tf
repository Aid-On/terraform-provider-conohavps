# ConoHa deletes a snapshot automatically 24 hours after it is created
resource "conohavps_volume_snapshot" "example" {
  volume_id   = "11111111-1111-1111-1111-111111111111" # Replace with your volume ID
  name        = "example-snapshot"
  description = "Snapshot before upgrade"
}
