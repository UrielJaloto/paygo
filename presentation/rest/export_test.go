package rest

var (
	DecodeJSON                = decodeJSON
	OwnerFrom                 = ownerFrom
	OwnerGroup                = newOwnerGroup
	AdminGroup                = newAdminGroup
	ErrMalformedRequest       = errMalformedRequest
	ErrPayloadTooLarge        = errPayloadTooLarge
	ErrUnsupportedMediaType   = errUnsupportedMediaType
	ErrRateLimited            = errRateLimited
	ErrIdempotencyKeyRequired = errIdempotencyKeyRequired
)
