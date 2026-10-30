package rest

var (
	DecodeJSON                = decodeJSON
	OwnerFrom                 = ownerFrom
	OwnerGroup                = ownerGroup
	AdminGroup                = adminGroup
	ErrMalformedRequest       = errMalformedRequest
	ErrPayloadTooLarge        = errPayloadTooLarge
	ErrUnsupportedMediaType   = errUnsupportedMediaType
	ErrRateLimited            = errRateLimited
	ErrIdempotencyKeyRequired = errIdempotencyKeyRequired
)
