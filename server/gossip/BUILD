load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "gossip",
    srcs = ["gossip.go"],
    importpath = "github.com/buildbuddy-io/buildbuddy/server/gossip",
    visibility = ["//visibility:public"],
    deps = [
        "//server/util/log",
        "//server/util/network",
        "@com_github_hashicorp_memberlist//:memberlist",
        "@com_github_hashicorp_serf//serf",
    ],
)

go_test(
    name = "gossip_test",
    srcs = ["gossip_test.go"],
    deps = [
        ":gossip",
        "//server/testutil/app",
        "@com_github_hashicorp_serf//serf",
        "@com_github_stretchr_testify//require",
    ],
)
