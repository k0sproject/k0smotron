package v1beta2

// Hub marks RemoteMachine as a conversion hub.
func (*RemoteMachine) Hub() {}

// Hub marks PooledRemoteMachine as a conversion hub.
func (*PooledRemoteMachine) Hub() {}
