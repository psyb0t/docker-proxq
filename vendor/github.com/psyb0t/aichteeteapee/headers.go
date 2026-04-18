package aichteeteapee

const (
	// Authentication headers.
	HeaderNameAuthorization = "Authorization"
	HeaderNameXAPIKey       = "X-Api-Key" //nolint: gosec

	// Authentication schemes.
	AuthSchemeBearer = "Bearer "

	// Content headers.
	HeaderNameContentType    = "Content-Type"
	HeaderNameContentLength  = "Content-Length"
	HeaderNameAccept         = "Accept"
	HeaderNameAcceptEncoding = "Accept-Encoding"

	// Request tracking.
	HeaderNameXRequestID     = "X-Request-ID"
	HeaderNameXCorrelationID = "X-Correlation-ID"

	// Client info.
	HeaderNameUserAgent       = "User-Agent"
	HeaderNameXForwardedFor   = "X-Forwarded-For"
	HeaderNameXForwardedProto = "X-Forwarded-Proto"
	HeaderNameXForwardedHost  = "X-Forwarded-Host"
	HeaderNameXRealIP         = "X-Real-IP"
	HeaderNameXClientID       = "X-Client-ID"

	// CORS headers.
	HeaderNameOrigin                        = "Origin"
	HeaderNameAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
	HeaderNameAccessControlAllowMethods     = "Access-Control-Allow-Methods"
	HeaderNameAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
	HeaderNameAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
	HeaderNameAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
	HeaderNameAccessControlMaxAge           = "Access-Control-Max-Age"
	HeaderNameVary                          = "Vary"

	// Cache control.
	HeaderNameCacheControl = "Cache-Control"
	HeaderNameETag         = "ETag"
	HeaderNameIfNoneMatch  = "If-None-Match"

	// Hop-by-hop headers (RFC 2616 section 13.5.1).
	// These must not be forwarded by proxies.
	HeaderNameConnection         = "Connection"
	HeaderNameKeepAlive          = "Keep-Alive"
	HeaderNameProxyAuthenticate  = "Proxy-Authenticate"
	HeaderNameProxyAuthorization = "Proxy-Authorization"
	HeaderNameTE                 = "Te"
	HeaderNameTrailers           = "Trailers"
	HeaderNameTransferEncoding   = "Transfer-Encoding"
	HeaderNameUpgrade            = "Upgrade"

	// Security headers.
	HeaderNameStrictTransportSecurity = "Strict-Transport-Security"
	HeaderNameXContentTypeOptions     = "X-Content-Type-Options"
	HeaderNameXFrameOptions           = "X-Frame-Options"
	HeaderNameXXSSProtection          = "X-XSS-Protection"
	HeaderNameReferrerPolicy          = "Referrer-Policy"
	HeaderNameContentSecurityPolicy   = "Content-Security-Policy"
	HeaderNameWWWAuthenticate         = "WWW-Authenticate"
)
