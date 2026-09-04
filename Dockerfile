FROM public.ecr.aws/docker/library/golang:1.27-alpine AS gobuild

ARG REVISION

WORKDIR /app

COPY go.mod .
COPY go.sum .
COPY vendor/ vendor
COPY main.go main.go

RUN --mount=type=cache,target=/root/.cache/go-build \
    go build -o=app -mod=vendor -ldflags \
    "-X 'main.buildTimestamp=$(date '+%b %d %Y %T')' -X main.revision=$REVISION" \
    main.go

FROM alpine:3.24

RUN apk --no-cache add ca-certificates bash coreutils

COPY --from=gobuild /app/app .

COPY assets assets

EXPOSE 8000

CMD [ "./app" ]
