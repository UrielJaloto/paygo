package rest

var (
	DecodeJSON                = decodeJSON
	OwnerFrom                 = readOwner
	OwnerGroup                = newOwnerGroup
	AdminGroup                = newAdminGroup
	ErrMalformedRequest       = errMalformedRequest
	ErrPayloadTooLarge        = errPayloadTooLarge
	ErrUnsupportedMediaType   = errUnsupportedMediaType
	ErrRateLimited            = errRateLimited
	ErrIdempotencyKeyRequired = errIdempotencyKeyRequired
)
