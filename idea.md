# Ideas to improve message rate

## Use p-CSMA

- Use p-CSMA to reduce the number of collisions by allowing nodes to transmit with a probability p.
- This can be done by modifying the CSMA algorithm to include a probability p for each node to transmit.

## Have a timeout once transmission has completed

- Once a node has completed its transmission, it should wait for a timeout period before attempting to transmit again.
- This will allow other nodes to have a chance to transmit and reduce the number of collisions.
- The timeout period can be adjusted based on the number of nodes in the network and the expected message rate.
- This can be done by adding a timer to each node that is reset after each transmission.
- The timer can be set to a random value between a minimum and maximum value to further reduce the chance of collisions.

This is quite similar to CSMA/CA with a backoff timer which is currently implemented in the code.

## Conditional response to RREQ and UREQ
- Nodes currently respond to RREQ and UREQ if they are the destination or if they have the route to the destination.
- This can be modified to only respond if the node is the destination or if it has a route to the destination and the route is not already being used by another node.
- ALternatively, nodes can respond to RREQ and UREQ with a probability p to reduce the number of responses and collisions.
- Even better, nodes only respond to RREQ and UREQ if the destination node is greater than n hops away. This will prevent the nodes all responding to the same RREQ and UREQ if they are all within n hops of the destination.
- N should be tuned but essentially means that the message is already so close to the destination that it is not worth responding to the RREQ or UREQ and we might as well just let the message continue on its way.