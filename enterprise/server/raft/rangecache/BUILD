load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "rangecache",
    srcs = ["rangecache.go"],
    importpath = "github.com/buildbuddy-io/buildbuddy/enterprise/server/raft/rangecache",
    visibility = ["//visibility:public"],
    deps = [
        "//enterprise/server/raft/constants",
        "//proto:raft_go_proto",
        "//server/util/log",
        "//server/util/rangemap",
        "//server/util/status",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_serf//serf",
    ],
)

go_test(
    name = "rangecache_test",
    srcs = ["rangecache_test.go"],
    deps = [
        ":rangecache",
        "//enterprise/server/raft/constants",
        "//proto:raft_go_proto",
        "//server/util/log",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_serf//serf",
        "@com_github_stretchr_testify//require",
    ],
)
