resource "conohavps_lb_loadbalancer" "lb_1" {
  name = "tf-example-lb-1"
}

resource "conohavps_lb_listener" "listener_1" {
  name            = "tf-example-listener-1"
  protocol        = "TCP"
  protocol_port   = 80
  loadbalancer_id = conohavps_lb_loadbalancer.lb_1.id
}
