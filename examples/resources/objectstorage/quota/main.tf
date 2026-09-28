# Contract 200 GB of object storage (billed per 100 GB).
# Destroying this resource sets the capacity back to 0 GB.
resource "conohavps_objectstorage_quota" "main" {
  quota_gb = 200
}
