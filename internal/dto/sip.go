package dto

type (
	Packet struct {
		IsResponse     bool
		ResponseStatus []byte
		Method         []byte
		From           From
		To             To
		CallID         []byte
		CSeq           CSeq
		SessionExpires int // seconds
	}
	From struct {
		Addr []byte
		Tag  []byte
	}
	To struct {
		Addr []byte
		Tag  []byte
	}
	CSeq struct {
		ID     []byte
		Method []byte
	}
)
