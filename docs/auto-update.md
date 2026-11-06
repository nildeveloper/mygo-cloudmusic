# 自家仓库自动更新

正式 Release 构建通过 MyGo 原生 updater 检查、下载和安装更新，地址为
`nildeveloper/mygo-cloudmusic`。菜单、状态栏和设置共享同一个更新入口。
本地构建的 `mygo.json` 不配置更新源，入口仍打开自家 Releases 下载页。
CI 注入公钥和仓库地址后才启用原生更新，避免测试包意外安装正式版。

## 一次性配置

在自己可信的电脑上，进入项目根目录运行（需要项目要求的 Go 工具链）：

```bash
go tool mygo keygen
```

命令会显示两个文件的位置：`mygo-update.pub` 和 `mygo-update.key`。
默认保存到系统用户配置目录下的 `mygo/update-keys`，无需放入项目。
将两个文件各自的**完整文本内容**配置到仓库的
[Settings → Secrets and variables → Actions](https://github.com/nildeveloper/mygo-cloudmusic/settings/secrets/actions)：

| 配置类型 | 名称 | 内容 |
| --- | --- | --- |
| Repository variable（Variables 页签） | `MYGO_UPDATER_PUBLIC_KEY` | `mygo-update.pub` 内容 |
| Repository secret（Secrets 页签） | `MYGO_UPDATER_PRIVATE_KEY` | `mygo-update.key` 内容 |

沿用现有 `MACOS_SIGNING_P12_BASE64` 和 `MACOS_SIGNING_P12_PASSWORD`，无需更换 P12。
更新密钥是 Ed25519 密钥对；P12 是 macOS 应用签名证书，各自独立。
实际被更新密钥签署的是更新归档的 SHA-256 摘要，签名存放在 JSON 清单内。

私钥不写入 Git、聊天或普通下载包，另行保存在密码管理器等安全备份位置。
以后各版本复用同一对密钥。更换公钥会让旧客户端拒绝新签名；不能每次发布重新生成。
`.gitignore` 已忽略 `mygo-update.key`，但其他命名的私钥仍需自行避免放入仓库。

## 发布过程与代码边界

- `.github/workflows/release.yml`：校验配置、版本与 tag；注入更新源；构建；P12 签名；上传；汇总校验。
- `scripts/sign-update/main.go`：仅在 macOS P12 签名后，刷新更新归档的大小与 Ed25519 签名；核对公私钥、版本、目标架构和下载地址。使用 Go 标准库，不新增依赖。
- `internal/app/updates.go`：有可用更新源时注册原生 updater，无更新源时打开 Releases 页。不改播放器业务。

构建使用的 `.updates` 配置如下，由 Actions 写入临时工作区的 `mygo.json`：

```json
{
  "publicKey": "Actions variable 的值",
  "github": "nildeveloper/mygo-cloudmusic",
  "deltas": 0
}
```

macOS 顺序：MyGo 构建 → DMG 内 P12 签名 → 签名/架构校验 → 从最终 DMG 中打包 `.app`
→ 解压校验应用签名与可执行文件一致 → 更新归档签名。
这样 DMG 安装和自动更新拿到同一份签名应用。当前关闭差量更新，避免差量补丁基于 P12 签名前的应用生成。
Windows/Linux 的归档与签名直接使用 MyGo 原生产物。

每个平台/架构上传安装包、`<slug>-<version>-<platform>-<arch>.tar.gz` 和
`update-<platform>-<arch>.json`。先上传归档，再上传清单。
全部构建成功后，核对六份清单的版本和归档大小，生成 `SHA256SUMS`（MyGo 安装脚本使用）及相同内容的 `SHA256SUMS.txt`。

工作流保留 **draft release**，待校验后手动公开。已公开的版本禁止覆盖，重试只允许更新草稿。
手动运行也要求当前 commit 就是配置版本对应的 tag；不从另一个分支混用发布说明或脚本。
发布时应同时更新 `mygo.json` 版本和 `CHANGELOG.md` 的对应版本节，再创建匹配的 tag。
`v0.1.27` 已公开，首次启用自动更新的版本为 `v0.1.28`；不能重跑覆盖旧版。

## 验证与首次接入

1. 配置密钥后，先构建新版本草稿，确认三个 build job 和 checksums 全部成功。
2. macOS 现有用户手动安装第一版支持自动更新的正式安装包，放入有写入权限的 Applications 目录。
   无更新源的旧版/本地测试包不会自动获得这项能力。
3. 再发布更高版本，使用前一版“检查更新”，核对版本说明、下载、安装与重启。
   草稿不会成为 `/releases/latest/download/` 的可见更新。
4. 核对登录状态、播放设置和数据目录保留；macOS 检查签名身份与前一版一致。
5. 若需公开前演练，使用专门测试仓库与同一对临时测试密钥构建 A/B 两个版本，
   在测试仓库依次公开 A/B；不要篡改正式版本号或覆盖正式更新资产。

macOS 包仍未经过 Apple 公证；自动更新签名不替代 P12 签名或 Apple 公证。
Linux 的系统包管理器安装位置可能没有写权限，这种情况下入口会打开 Releases 页。
当前环境可验证归档签名兼容性、解压内容和构建；macOS DMG 操作、真实 P12 签名与跨版本重启安装需要 CI/真机验证。

## 本次本地验证（2026-11-06）

- actionlint 与工作流 shell 语法检查通过，MyGo vet 通过。
- 本地隔离测试连续 5 次通过：公私钥不匹配、私钥内部不一致、版本/URL/架构错误、空归档及差量误配置均拒绝；更新签名覆盖最终归档，保留版本说明。
- 用一次性测试密钥构建 ARM64 应用，重新签名、打包后刷新更新清单；使用当前固定版本 MyGo 的 `Verify` 与 `ExtractArchive` 验证通过，6 个 bundle 文件内容和权限一致；错误公钥与归档篡改被拒绝。测试私钥已删除，测试包未上传。
- `go test ./...` 仍有已有的 5 个失败：`TestDownloadModelFromCaptions`、`TestCaptionClickSeeks`、`TestCaptionsOnTheirWay`、`TestNotchGreetsOnce`、`TestNotchGreetingKeptUnderPointer`。
- 上述测试代码保留在本地忽略目录，未加入 Git。真实 P12/DMG CI 流程、跨版本安装与重启尚待配置正式密钥后验证。
