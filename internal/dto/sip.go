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
		Expires        int // seconds (RFC 3261 §20.19, registration binding TTL)
		UserAgent      []byte
		ContentType    []byte
		Body           []byte
	}
	From struct {
		User []byte
		Addr []byte
		Tag  []byte
	}
	To struct {
		User []byte
		Addr []byte
		Tag  []byte
	}
	CSeq struct {
		ID     []byte
		Method []byte
	}
)
