resource "conohavps_lb_loadbalancer" "lb_1" {
  name = "tf-example-lb-1"
}

resource "conohavps_lb_listener" "listener_1" {
  name            = "tf-example-listener-1"
  protocol        = "TCP"
  protocol_port   = 80
  loadbalancer_id = conohavps_lb_loadbalancer.lb_1.id
}

resource "conohavps_lb_pool" "pool_1" {
  name         = "tf-example-pool-1"
  protocol     = "TCP"
  lb_algorithm = "ROUND_ROBIN"
  listener_id  = conohavps_lb_listener.listener_1.id
}
