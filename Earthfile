VERSION 0.8

IMPORT github.com/formancehq/earthly:v0.16.0 AS core

FROM core+base-image

CACHE --sharing=shared --id go-mod-cache /go/pkg/mod
CACHE --sharing=shared --id golangci-cache /root/.cache/golangci-lint
CACHE --sharing=shared --id go-cache /root/.cache/go-build

sources:
    WORKDIR src
    COPY --dir . /src
    SAVE ARTIFACT /src

tidy:
    FROM core+builder-image
    CACHE --id go-mod-cache /go/pkg/mod
    CACHE --id go-cache /root/.cache/go-build
    WORKDIR /src
    COPY --dir (+sources/src/*) /src
    DO --pass-args core+GO_TIDY
    SAVE ARTIFACT /src
    SAVE ARTIFACT go.* AS LOCAL ./

lint:
    FROM core+builder-image
    CACHE --id go-mod-cache /go/pkg/mod
    CACHE --id go-cache /root/.cache/go-build
    CACHE --id golangci-cache /root/.cache/golangci-lint
    WORKDIR /src
    COPY --dir (+tidy/src/*) /src
    DO --pass-args core+GO_LINT
    SAVE ARTIFACT ./* AS LOCAL ./

tests:
    FROM core+builder-image
    CACHE --id go-mod-cache /go/pkg/mod
    CACHE --id go-cache /root/.cache/go-build
    WORKDIR /src
    COPY --dir (+tidy/src/*) /src
    WITH DOCKER
        DO --pass-args core+GO_TESTS
    END

pre-commit:
    WAIT
      BUILD --pass-args +tidy
    END
    BUILD --pass-args +lint

generate:
    FROM core+builder-image
    CACHE --id go-mod-cache /go/pkg/mod
    CACHE --id go-cache /root/.cache/go-build
    RUN apk update && apk add openjdk11
    DO --pass-args core+GO_INSTALL --package=go.uber.org/mock/mockgen@latest
    WORKDIR /src
    COPY --dir (+tidy/src/*) /src
    DO --pass-args core+GO_GENERATE
    SAVE ARTIFACT ./* AS LOCAL ./

ci:
  LOCALLY
  BUILD +pre-commit
  BUILD +tests