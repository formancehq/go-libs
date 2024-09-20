VERSION 0.8

IMPORT github.com/formancehq/earthly:feat/monorepo AS core

FROM core+base-image

sources:
    WORKDIR src
    COPY --dir . /src
    SAVE ARTIFACT /src

tidy:
    FROM core+builder-image
    WORKDIR /src
    COPY --dir (+sources/src/*) /src
    DO --pass-args core+GO_TIDY
    SAVE ARTIFACT /src
    SAVE ARTIFACT go.* AS LOCAL ./

lint:
    FROM core+builder-image
    WORKDIR /src
    COPY --dir (+tidy/src/*) /src
    DO --pass-args core+GO_LINT
    SAVE ARTIFACT * AS LOCAL ./

tests:
    FROM core+builder-image
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
    RUN apk update && apk add openjdk11
    DO --pass-args core+GO_INSTALL --package=go.uber.org/mock/mockgen@latest
    WORKDIR /src
    COPY --dir (+tidy/src/*) /src
    DO --pass-args core+GO_GENERATE
    SAVE ARTIFACT * AS LOCAL ./
