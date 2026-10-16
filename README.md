# pocket-ssh-agent

SSH の秘密鍵をスマホに置き、PC で `git push` や `ssh` をするたびに**スマホで承認**する SSH エージェントです。
PC には鍵が存在しないので、PC からパスフレーズや鍵ファイルを盗まれても使えません。

```
PC                                                   スマホ
ssh / git ──▶ pocket-ssh-agent ◀──https (Tailscale)── PWA（GitHub Pages）
              ・名前付きパイプ / Unix ソケット            ・Ed25519 鍵（取り出し不可の WebCrypto 鍵）
              ・署名要求をスマホへ中継                    ・内容を自分で解析して表示
              ・返ってきた署名を検証                      ・承認したときだけ署名
```

- PC 側は Go 製の単体バイナリ（Windows / macOS / Linux）。
- スマホ側はインストール不要の PWA。<https://tksugimoto-ai.github.io/pocket-ssh-agent/>
- 通信は Tailscale の tailnet 内だけで完結し、中継サーバーはありません。
- 鍵は普通の `ssh-ed25519` なので、GitHub を含むどの SSH サーバーでも使えます。

> **状態: 実験的な初期版です。** 実機（Windows + iPhone）での検証はこれからです。

## 守れるもの・守れないもの

| | |
|---|---|
| ✅ PC のマルウェアに鍵を盗まれる | 鍵はスマホの中だけ。PC には公開鍵しかありません |
| ✅ 知らないうちに鍵を使われる | 署名のたびにスマホでの承認が必要です |
| ✅ 汎用の署名器として悪用される | スマホは「SSH ログイン」と「SSHSIG（git 署名など）」以外の形式には署名しません |
| ✅ PC の他ユーザーや tailnet 上の他端末からの割り込み | ペアリング後は共有鍵による HMAC で双方向に認証します。agent の停止中にポートを乗っ取られても、要求を偽造したり認証情報を盗んだりできません |
| ⚠️ 承認のタイミングに紛れ込まれる | 侵害された PC は、あなたが push した瞬間に別の要求を送れます。件数や接続先の表示、カードが出てから 1 秒間は承認できない仕組みで気づけるようにしていますが、完全には防げません（YubiKey などでも同じです） |
| ⚠️ `ssh -A` の転送先からの要求 | 転送された agent 経由の要求には警告を表示します |
| ⚠️ 接続先の表示 | 「署名で確認済み」は署名データに含まれるホスト鍵なので信頼できます。「PC の申告」は PC が自己申告したもので、侵害された PC は偽れます |
| ❌ スマホ自体の侵害 | スマホやブラウザが乗っ取られた場合は守れません |

PWA は PC ではなく GitHub Pages から配信します。PC から配信すると、侵害された PC が「承認画面なしで署名する偽の PWA」を配信できてしまうためです。

> **重要: PWA のオリジンを他のサイトと共有しないでください。**
> 鍵は「取り出せない」設定ですが、同じオリジン（`https://<アカウント>.github.io`）で動く JavaScript からは承認なしで署名に使えてしまいます。
> GitHub Pages では、同じアカウントのすべての Pages サイトが同じオリジンになります。
> このアカウントで他の Pages サイトを公開する（している）場合は、PWA を**独自ドメイン（サブドメイン可）**で配信し、agent の `-web-url` で指定してください。

## 必要なもの

- [Tailscale](https://tailscale.com/)（PC とスマホの両方で同じ tailnet に参加）
  - 管理画面の DNS 設定で **MagicDNS** と **HTTPS Certificates** を有効にしてください
- スマホ: iOS 17 以降の Safari、または最近の Chrome（WebCrypto の Ed25519 が必要）

## セットアップ

### 1. PC で agent を起動

[Releases](https://github.com/tksugimoto-ai/pocket-ssh-agent/releases) から OS に合ったバイナリをダウンロードします（Go がある場合は `go install github.com/tksugimoto-ai/pocket-ssh-agent/cmd/pocket-ssh-agent@latest` でも可）。

```powershell
pocket-ssh-agent
```

初回はペアリング用の QR コードが表示されます。別のターミナルで、agent を tailnet に公開します（初回だけ）。

```powershell
tailscale serve --bg --https=7766 http://127.0.0.1:7766
```

agent 自身は `127.0.0.1` でしか待ち受けません。TLS は Tailscale が正規の証明書で終端します。

### 2. スマホでペアリング

1. スマホで <https://tksugimoto-ai.github.io/pocket-ssh-agent/> を開きます。
2. **iPhone の場合は先に「ホーム画面に追加」して、追加したアプリから開いてください。** Safari とホーム画面のアプリは保存領域が別で、Safari の保存データはしばらく使わないと消えることがあります。
3. アプリの「QR コードを読み取る」で、PC に表示された QR を読み取ります。
   （カメラアプリで読み取ると Safari で開いてしまうため、アプリ内で読み取ってください。Android はどちらでも可）
4. 「ペアリングする」を押すと鍵が作られ、公開鍵が表示されます。

### 3. 公開鍵を登録

表示された公開鍵（PC でも `pocket-ssh-agent pubkey` で表示可）を、GitHub の **Settings → SSH and GPG keys** や、接続先サーバーの `authorized_keys` に登録します。

### 4. SSH クライアントから使う

agent を起動したまま、SSH クライアントに agent の場所を教えます。

**Windows（OpenSSH）**

```powershell
$env:SSH_AUTH_SOCK = '\\.\pipe\pocket-ssh-agent'
ssh -T git@github.com
```

Git for Windows 同梱の ssh は名前付きパイプの agent を使えないので、Windows の OpenSSH を使うよう設定します。

```powershell
git config --global core.sshCommand "C:/Windows/System32/OpenSSH/ssh.exe"
```

**macOS / Linux**

```sh
export SSH_AUTH_SOCK="$HOME/.pocket-ssh-agent/agent.sock"   # 起動時のログに表示されるパス
ssh -T git@github.com
```

スマホで PWA を開いておくと承認画面が出ます。承認すると接続が進みます。

### devcontainer で使う（未検証）

VS Code の Dev Containers は、Windows 標準の ssh-agent（`\\.\pipe\openssh-ssh-agent`）をコンテナへ転送します。
Windows の ssh-agent サービスを止めて、pocket-ssh-agent をその名前で起動すると、コンテナ内からも使える見込みです。

```powershell
Stop-Service ssh-agent
Set-Service ssh-agent -StartupType Disabled
pocket-ssh-agent -socket '\\.\pipe\openssh-ssh-agent'
```

### git のコミット署名にも使う（任意）

```sh
git config --global gpg.format ssh
git config --global user.signingkey "key::$(pocket-ssh-agent pubkey)"
git config --global commit.gpgsign true
```

## オプション

```
pocket-ssh-agent [flags]
  -listen      127.0.0.1:7766   スマホ用 API の待ち受け（ループバックのみ）
  -socket      agent のソケット（Windows は名前付きパイプ）
  -public-url  スマホから見た URL（既定: https://<MagicDNS 名>:<listen のポート>）
  -web-url     PWA の URL（独自ドメインで配信する場合に変更）
  -name        スマホに表示する PC 名（既定: ホスト名）
  -timeout     承認を待つ時間（既定: 90s）
  -pair        ペアリングし直す（スマホを替えたときなど）
pocket-ssh-agent pubkey        ペアリング済みの公開鍵を表示
```

## 制限事項

- 承認するには、スマホで PWA を**開いたまま**にしておく必要があります（通知は今後対応予定）。
- PWA のデータ（鍵）を消すと鍵は復元できません。新しい鍵を作って登録し直してください。
- ペアリングできるスマホは PC 1 台につき 1 台です。
- スマホと PC の時計が 2 分以上ずれていると接続できません（認証にタイムスタンプを使うため）。
- Android の Chrome では、初回に「ローカル ネットワークへのアクセス」の許可を求められることがあります。許可しないと PC に接続できません。

## 今後

- Web Push による承認通知（PC から直接送信。中継サーバーは不要）
- 承認時の生体認証（WebAuthn）と、パスキー PRF による鍵の暗号化
- 要求元プロセス名の表示

## 通信の仕組み

1. **ペアリング**: PC が QR に「接続先 URL」と「使い捨てコード（128 bit）」を載せます。スマホは鍵を作り、公開鍵とコードを送ります。PC は 32 バイトの共有鍵を返し、コードで作った HMAC を添えて「QR を表示した本物の PC からの応答」であることを証明します。
2. **以降の通信**: 共有鍵そのものは二度と送りません。
   - スマホ → PC: リクエストごとに、メソッド・パス・時刻・ノンス・本文のハッシュに対する HMAC を付けます（再送は拒否）。
   - PC → スマホ: 署名要求ごとに HMAC を付け、スマホは検証できない要求を無視します。
3. **署名**: スマホは要求データを自分で解析し、許可した形式だけを、ユーザーが承認したときに署名します。PC は返ってきた署名を公開鍵で検証してから SSH クライアントに返します。

スマホ側では共有鍵を取り出し不可の HMAC 鍵として、SSH 鍵を取り出し不可の Ed25519 鍵として IndexedDB に保存します。

## 開発

```sh
go test -race ./...        # Go のテスト
npm ci && npm test         # PWA ライブラリのテスト（Node 22+）
bash scripts/e2e.sh        # agent + 擬似スマホ + OpenSSH（ssh / sshd が必要）
npx playwright install chromium && node test/e2e/ui.mjs   # 実際の PWA を Chromium で操作
```

- PC 側: `cmd/pocket-ssh-agent`, `internal/`
- スマホ側: `web/`（ビルド不要の素の ES モジュール）。QR 読み取りに [jsQR](https://github.com/cozmo/jsQR) 1.4.0（Apache-2.0）を `web/vendor/jsqr/` に同梱しています。
- アイコン: `python3 scripts/gen-icons.py`

## ライセンス

MIT
