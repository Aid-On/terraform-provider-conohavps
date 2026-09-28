# The account needs an object storage capacity before objects can be stored
resource "conohavps_objectstorage_quota" "main" {
  quota_gb = 100
}

# Container that keeps the old versions of objects
resource "conohavps_objectstorage_container" "archive" {
  name = "photos-archive"
}

# Container published on the web, with object versioning
resource "conohavps_objectstorage_container" "photos" {
  name              = "photos"
  versions_location = conohavps_objectstorage_container.archive.name
  web_publishing    = true
}
