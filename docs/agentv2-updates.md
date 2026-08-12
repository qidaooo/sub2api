# agentv2 更新链路

这个 fork 保留两条分支：

- main：官方上游分支，作为 fork 的同步镜像。
- agentv2：生产分支，基于上游 release tag 叠加 multi-agent v2 兼容补丁。

UPSTREAM_RELEASE 记录当前生产分支的上游基线。Sync latest upstream release
工作流每天检查 Wei-Shaw/sub2api 的最新 release，把 agentv2 的补丁提交
重放到新 tag 上。补丁发生冲突时工作流失败并停止，不会自动发布不完整版本。

生产分支更新后，Build agentv2 image 会运行 Go 测试并构建以下镜像：

    ghcr.io/qidaooo/sub2api:agentv2
    ghcr.io/qidaooo/sub2api:<upstream-version>

这个镜像的 BuildType 是 custom，内置官方二进制更新器会被禁用。
服务器更新应使用 GitHub Actions 构建出的镜像，或由运维手动拉取该镜像；
不要在 Sub2API 面板中直接安装官方 release 二进制。

在部署环境开启功能：

    GATEWAY_CODEX_MULTI_AGENT_V2_ENABLED=true
