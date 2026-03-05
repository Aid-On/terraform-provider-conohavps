# Add volume: Create an add volume
resource "conohavps_volume" "add_volume" {
  name        = "example-add-volume"
  size        = 200
  volume_type = "c3j1-ds02-add"
  description = "Example add volume"
}

# Boot volume: Create from an image
resource "conohavps_volume" "boot_volume" {
  name        = "example-boot-volume"
  size        = 100
  volume_type = "c3j1-ds02-boot"
  image_ref   = "11111111-1111-1111-1111-111111111111" # Replace with your saved image ID
  description = "Boot volume from image"
}

# Add volume: Create from saved image
resource "conohavps_volume" "add_volume_from_image" {
  name        = "example-image-volume"
  size        = 200
  volume_type = "c3j1-ds02-add"
  image_ref   = "11111111-1111-1111-1111-111111111111" # Replace with your saved image ID
  description = "Volume from saved image"
}

# Clone: Create a volume from an existing volume
resource "conohavps_volume" "cloned_volume" {
  name         = "example-cloned-volume"
  size         = conohavps_volume.add_volume.size
  volume_type  = conohavps_volume.add_volume.volume_type
  source_volid = conohavps_volume.add_volume.id
  description  = "Cloned from volume"
}

# Restore: Create a volume from a backup
resource "conohavps_volume" "restored_volume" {
  name        = "example-restored-volume"
  size        = 100 # Size must be equal to or greater than the backup source
  volume_type = "c3j1-ds02-boot"
  backup_id   = "11111111-1111-1111-1111-111111111111" # Replace with your backup ID
  description = "Restored from backup"
}
