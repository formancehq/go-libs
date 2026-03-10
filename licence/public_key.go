package licence

// formancePublicKey is the PEM-encoded RSA public key used to verify licence JWTs (RS256).
// This key is fetched from https://license.formance.cloud/keys and controlled by Formance.
// Only Formance holds the corresponding private key and can sign valid licence tokens.
//
// It can be overridden at build time via ldflags for staging or key rotation:
//
//	go build -ldflags "-X github.com/formancehq/go-libs/v4/licence.formancePublicKey=$(cat custom_key.pem)"

//nolint:lll
var formancePublicKey = `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA46LVe+BCO/go0MoKM4r7
exTGeFSz10ra/hpFK0XJGVm6W42GTjFzNlNTCKQZBkF63STYK+o+FEFmSgMVxTjf
qA4GZGxYddukT4pNR+WaRLQSPxPkMsGrzoORtq8n2v4Y+m5jvYDXhLLmYsDNxVuv
SrAOtgJ0Ac8jJWXEu8Eqs0ferl9ftLRqrN+RfpXATT4fAgHBxVl5u1mFsQX6lo1B
N5m099Ni50Cmlauun883bS8xzLt/XLlk6vBaJKhfyDbkjcA4qN+33f5mih4v6EBP
txyeCg9yhHOfga61owAI+FOGEVW1OMTQ3PP/d2buiw9YrRAtBEXsJdhovc84jwmJ
sjA829+2nFR1Bq3jQ8nG4iTnF9yIwJr+l9reoV8Butskwld9mhry+dIimGpVUmy3
psYmj910D1eH+tyuCGN7YAjD5+bXVUBPGfD1kJExtzjjyYruXD6trt7nchWrJIOu
D1I0OT3j+PWASm0c/AdN8BcV96HZhJBbCDK5GaQ9HSw+GVEpaqP9TY4uEz2werNq
cvjYlBS4FocA0ClsaDs9llIZVrI7kPYIeoO2KNWn7kp1q+awrNt677MLFmj7eqZ/
jl/Sx2brq8e91kTG57Z2qRTkSGkCK20NFOI8E+m9bhhVRFw4RhY6g3lH1B5hd+dd
6TCk5eN7hTkosG21POe9goUCAwEAAQ==
-----END PUBLIC KEY-----`
