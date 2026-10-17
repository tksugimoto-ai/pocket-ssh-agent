# Dev Container

[tksugimoto/my-devcontainer](https://github.com/tksugimoto/my-devcontainer) を git submodule
(`.devcontainer/my-devcontainer`) として取り込み、その Feature を使っています。

- Go (最新) / Node.js 24 / GitHub CLI / Claude Code
- my-devcontainer のカスタム Feature: `claude-code-bell`, `claude-code-statusline`,
  `claude-config-volume`, `node-modules-volume`
- `gh-auth-volume` だけはボリューム名をこのプロジェクト用 (`pocket-ssh-agent-gh-auth`) にするため
  `.devcontainer/features/gh-auth-volume` にコピーして使っています

## 使い方

submodule を含めて取得してから、VS Code で「Dev Containers: Reopen in Container」を実行します。

```bash
git clone --recurse-submodules https://github.com/tksugimoto-ai/pocket-ssh-agent.git
# 既に clone 済みの場合
git submodule update --init
```

初回のみ、別のターミナルで `gh auth login` と `gh auth setup-git` を実行してください。

## my-devcontainer の更新

```bash
git submodule update --remote .devcontainer/my-devcontainer
```

`devcontainer-lock.json` は Dev Container のビルド (VS Code の Rebuild や `devcontainer up`) のたびに
`devcontainer.json` に合わせて書き直されます。Feature を足す・外すとエントリも自動で増減するので、
再作成後に差分が出ていればそのままコミットしてください。

ロック済みの Feature のバージョンは再作成しても上がりません。上げるときは次を実行してコミットします。

```bash
npx @devcontainers/cli upgrade --workspace-folder .
```
