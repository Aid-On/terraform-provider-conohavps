# Raise the image save capacity from the free 50 GB to 550 GB (billed per 500 GB added).
# Destroying this resource sets the capacity back to 50 GB.
resource "conohavps_image_quota" "main" {
  image_size_gb = 550
}
