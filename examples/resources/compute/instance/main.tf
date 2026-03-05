# Instance creation with attached volume, security group, and keypair
resource "conohavps_instance" "test" {
  instance_name_tag = "test-instance" # Instance name tag
  admin_pass        = "xxxxxxxxxxxx" # Admin password for the instance
  flavor_id         = "f2a77529-1815-43a2-bc14-1f3f6b09079c" # Flavor ID for the instance
  block_device =  [
    {
      uuid = conohavps_volume.test.id # Volume ID to attach
    }
  ]
  security_group = [
    {
      name = conohavps_securitygroup.test.name # Security group name
    },
  ]
  key_name = conohavps_keypair.test.name # Keypair name
  power_state = "<ACTIVE or SHUTOFF>" # Desired power state of the instance
}

# Volume creation
resource "conohavps_volume" "test" {
  size      = 200 # Size in GB
  image_ref = "884c1899-fefe-40bd-aab8-1001b1d9c895" # image UUID for boot volume
  description = "Test boot volume from image" # Volume description if needed
  name      = "test-boot-volume" # Volume name
  volume_type = "c3j1-ds02-boot" # Volume type
}

# keypair creation
resource "conohavps_keypair" "test" {
  name = "keypair_test" # Keypair name
  public_key = "ssh-rsa xxxxxxxxxxxxxxxxxx..." # Replace with your actual public key
}

# security group creation
resource "conohavps_securitygroup" "test" {
  name        = "security_group_test" # Security group name
  description = "Test security group" # Security group description if needed
}