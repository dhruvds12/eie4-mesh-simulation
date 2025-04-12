package mesh

type INetwork interface {
	Run()
	Join(n INode)
	Leave(nodeID uint32)
	BroadcastMessage(msg []byte, sender INode, packetID uint32)
	UnicastMessage(msg []byte, sender INode, packetID uint32, to uint32)
	IsChannelFree(node INode) bool
	LeaveAll()
	GetNode(nodeId uint32) (INode, error)
}
