FROM golang:1.8.2

# Build this docker at the root of the project with
# docker build -f ./build/images/mx_operator/Dockerfile -t <tag> .

# Install dep - golang dependencies manager
#RUN curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
RUN curl https://glide.sh/get | sh

RUN mkdir -p /opt/operator && \
    mkdir -p /src/tf-operator
COPY . /src/tf-operator

# build setup
RUN mkdir -p /go/src/github.com/kubeflow && \
    ln -sf /src/tf-operator /go/src/github.com/kubeflow/tf-operator

WORKDIR /go/src/github.com/kubeflow/tf-operator

# resolve dependencies
RUN glide install -v && \
    go get github.com/sabhiram/go-tracey

WORKDIR /opt/operator

# build mx-operator
RUN go build github.com/kubeflow/tf-operator/cmd/mx-operator

# RUN chmod a+x /opt/operator/mx-operator
ENTRYPOINT ["/opt/operator/mx-operator", "-alsologtostderr"]
