load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "nodeliveness",
    srcs = ["nodeliveness.go"],
    importpath = "github.com/buildbuddy-io/buildbuddy/enterprise/server/raft/nodeliveness",
    visibility = ["//visibility:public"],
    deps = [
        "//enterprise/server/raft/client",
        "//enterprise/server/raft/constants",
        "//enterprise/server/raft/keys",
        "//enterprise/server/raft/rbuilder",
        "//proto:raft_go_proto",
        "//server/util/status",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_serf//serf",
    ],
)

go_test(
    name = "nodeliveness_test",
    srcs = ["nodeliveness_test.go"],
    deps = [
        ":nodeliveness",
        "//enterprise/server/raft/constants",
        "//proto:raft_go_proto",
        "//server/util/log",
        "//server/util/status",
        "@com_github_stretchr_testify//require",
        "@go_googleapis//google/rpc:status_go_proto",
        "@org_golang_google_grpc//status",
    ],
)
