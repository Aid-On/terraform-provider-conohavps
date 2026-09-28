# How much of the image save capacity the saved images use
data "conohavps_image_usage" "current" {}

output "image_usage_gb" {
  value = data.conohavps_image_usage.current.size_bytes / 1073741824
}
