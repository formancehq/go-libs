package client

import (
	"context"
	"net/http"

	httphelper "github.com/formancehq/go-libs/v4/oidc/http"
)

// Userinfo will call the OIDC [UserInfo] Endpoint with the provided token and returns
// the response in an instance of type U.
// [*oidc.UserInfo] can be used as a good example, or use a custom type if type-safe
// access to custom claims is needed.
//
// [UserInfo]: https://openid.net/specs/openid-connect-core-1_0.html#UserInfo
func Userinfo[U SubjectGetter](ctx context.Context, token, tokenType string, rp RelyingParty) (userinfo U, err error) {
	var nilU U

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rp.UserinfoEndpoint(), nil)
	if err != nil {
		return nilU, err
	}
	req.Header.Set("authorization", tokenType+" "+token)
	if err := httphelper.HttpRequest(rp.HttpClient(), req, &userinfo); err != nil {
		return nilU, err
	}

	return userinfo, nil
}
