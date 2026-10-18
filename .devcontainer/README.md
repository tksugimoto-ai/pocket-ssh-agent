# Dev Container

[tksugimoto/my-devcontainer](https://github.com/tksugimoto/my-devcontainer) を git submodule
(`.devcontainer/my-devcontainer`) として取り込み、その Feature を使っています。

- Go (最新) / Node.js 24 / GitHub CLI / Claude Code
- my-devcontainer のカスタム Feature: `claude-code-bell`, `claude-code-statusline`,
  `claude-config-volume`, `node-modules-volume`, `gh-auth-volume`

### ボリューム名

my-devcontainer の Feature が宣言するボリューム名は、`devcontainer.json` の `mounts` で同じ `target` を指定して
このプロジェクト用の名前に上書きしています (`mounts` は `target` ごとに後勝ちで、`devcontainer.json` は Feature より後に適用されます)。

| target | Feature の既定 | このプロジェクト |
|---|---|---|
| `/home/vscode/.claude` | `claude-config-${devcontainerId}` | `pocket-ssh-agent-claude-config-${devcontainerId}` |
| `/home/vscode/.config/gh` | `my-devcontainer-gh-auth` | `pocket-ssh-agent-gh-auth` |

`gh-auth-volume` の既定名のままだと、my-devcontainer を使う他のプロジェクトと gh のトークンを共有してしまうためです。
`pocket-ssh-agent-gh-auth` は固定名なので、このリポジトリの worktree 同士では共有します。

また、コンテナ内で pocket-ssh-agent を動かすための設定も入っています（[コンテナ内で agent を動かす](#コンテナ内で-agent-を動かす)）。

| target | ボリューム |
|---|---|
| `/home/vscode/.config/pocket-ssh-agent` | `pocket-ssh-agent-state` |

## 使い方

submodule を含めて取得してから、VS Code で「Dev Containers: Reopen in Container」を実行します。

```bash
git clone --recurse-submodules https://github.com/tksugimoto-ai/pocket-ssh-agent.git
# 既に clone 済みの場合
git submodule update --init
```

初回のみ、別のターミナルで `gh auth login` と `gh auth setup-git` を実行してください。

## コンテナ内で agent を動かす

ホストの agent を使う代わりに、開発中の pocket-ssh-agent をコンテナ内で動かして試せます。
agent は tsnet で自分自身を tailnet に参加させるので、コンテナに Tailscale は不要です。

```bash
go run ./cmd/pocket-ssh-agent -web-url https://<PWA の URL>/   # -web-url は初回のみ
```

- 初回はログの `https://login.tailscale.com/...` をホストのブラウザで開いて承認し、表示された QR コードでスマホとペアリングします。
- 別のターミナルで `export SSH_AUTH_SOCK=$HOME/.pocket-ssh-agent/agent.sock` を設定すると、そのターミナルの `ssh` / `git` がコンテナ内の agent を使います
  （設定しないターミナルでは、従来どおり VS Code が転送するホストの agent を使います）。
- 設定と tailnet の状態（`~/.config/pocket-ssh-agent`）はボリューム `pocket-ssh-agent-state` に保存されるので、
  コンテナを作り直してもペアリングと tailnet 上の端末（`pocket-ssh-devcontainer`）はそのままです。
  ホスト名を `devcontainer` に固定しているのはこのためです（既定ではコンテナ ID になり、作り直すたびに変わります）。
- ボリュームは固定名なので worktree 同士で共有します。同じ tailnet の状態を使うので、agent は一度に 1 つのコンテナでだけ起動してください。
- 状態を消してやり直すときは、コンテナを止めてから `docker volume rm pocket-ssh-agent-state` を実行します
  （Tailscale 管理画面の Machines から古い端末も削除してください）。

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
