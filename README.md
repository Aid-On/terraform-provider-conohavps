<div align="center">
  <img src="./assets/ConoHaVPS_logo.png" title="ConoHa VPS" height="150" />
</div>

# Terraform ConoHa VPS Provider

> [!NOTE]
> これは [gmo-internet/terraform-provider-conohavps](https://github.com/gmo-internet/terraform-provider-conohavps) を Aid-On が fork し、ConoHa VPS（Ver.3.0）の公開 API が提供する範囲のリソースとデータソースを追加したものです（Apache License 2.0）。追加分は GMO インターネット株式会社の公式提供ではありません。変更点は [CHANGELOG.md](CHANGELOG.md) を参照してください。

- Terraform ウェブサイト: https://developer.hashicorp.com/terraform
- ドキュメント: [docs/](docs/)（fork で追加した分を含む。Registry の https://registry.terraform.io/providers/gmo-internet/conohavps/latest/docs は fork 元の範囲だけ）

Terraform ConoHa VPS Provider は、Terraform が [ConoHa VPS](https://vps.conoha.jp/) 上のリソースを管理できるようにするプラグインです。

現在、下記リソースの管理に対応しています（★ はこの fork で追加したもの）。

| 分類 | リソース | データソース |
| --- | --- | --- |
| サーバー | サーバー、SSHキーペア、★ポートの接続、★自動バックアップ | ★フレーバー（プラン）、★イメージ |
| ボリューム | ボリューム、★ボリュームの接続、★スナップショット | ★バックアップ一覧 |
| ネットワーク | セキュリティグループ、セキュリティグループルール、★ローカルネットワーク、★サブネット、★ポート、★追加IP | ★QoS ポリシー |
| ロードバランサー | ★ロードバランサー、★リスナー、★プール、★メンバー、★ヘルスモニター | |
| DNS | ★ドメイン、★レコード | ★ドメイン |
| オブジェクトストレージ | ★コンテナ、★容量 | |
| イメージ | ★イメージ保存容量 | ★イメージ保存の使用量 |
| アイデンティティ | ★ロール、★サブユーザー、★S3 互換のアクセスキー | ★パーミッション一覧 |

追加分は、公開 API のドキュメントに沿って実装し、偽の API に対するテストで確認しています。2026-09-29 に全リソース・全データソースを実際の ConoHa アカウントで作成・読み戻し・破棄まで通しました（更新と import は偽の API でのみ確認）。

詳細については [docs/](docs/) を参照してください。

LLM やエージェント向けの入口は [llms.txt](llms.txt) です（セットアップ、全リソース・データソースの文書へのリンク、フレーバー名の読み方、2026-09-28 時点の ConoHa の料金）。料金が今も ConoHa のページに載っているかは `go run ./tools/llmsprices` で確かめられます（`pdftotext` が必要。`brew install poppler`）。

> [!IMPORTANT]
> Terraform ConoHa VPS Provider は、APIユーザーの認証情報を使用します。APIユーザーの作成については、[APIユーザーを作成する](https://doc.conoha.jp/reference/api-vps3/api-cp-vps3/cp-create_api_user-v3/) を参照してください。

> [!WARNING]
>  本ソフトウェアは現在ベータ版です。機能や動作が予告なく変更される場合があります。本番環境での使用前には十分なテストを行ってください。このベータ版 Terraform ConoHa VPS Provider を使用することで、これらの条件に同意したものとみなされます。

## セットアップ

この fork は Terraform Registry に公開していないため、手元でビルドして `dev_overrides` で使います。

### 1. ConoHa で API ユーザーを作る

[コントロールパネル](https://manage.conoha.jp/) の「API」ページで API ユーザーを作り、次の 3 つを控えます。

| 値 | 場所 |
| --- | --- |
| テナント ID | API ページ上部の「テナント情報」 |
| ユーザー ID | 作った API ユーザーの「ユーザーID」（32 桁の hex。ユーザー名ではない） |
| パスワード | API ユーザー作成時に決めたもの |

### 2. プロバイダをビルドして差し替える

```sh
go install .
cat >> ~/.terraformrc <<'X'
provider_installation {
  dev_overrides {
    "registry.terraform.io/gmo-internet/conohavps" = "/Users/<you>/go/bin"
  }
  direct {}
}
X
```

`go env GOPATH` の `bin` を指します。`terraform init` は Registry の v0.1.0 をロックファイルに書きますが、実行時は上書き先のバイナリが使われ、その旨の警告が出ます。fork を直したら `go install .` し直すだけで反映されます。

### 3. 認証情報を環境変数で渡す

```sh
cat > env.sh <<'X'
export CONOHAVPS_TENANT_ID='<テナント ID>'
export CONOHAVPS_USER_ID='<ユーザー ID>'
export CONOHAVPS_PASSWORD='<パスワード>'
X
chmod 600 env.sh
set -a; . ./env.sh; set +a
```

`env.sh` は共有しない（`.gitignore` に入れる）。`provider` ブロックに直接書くこともできますが、環境変数が優先されます。`region` の既定は `c3j1` です。

### 4. 最小構成を書く

```hcl
terraform {
  required_providers {
    conohavps = { source = "gmo-internet/conohavps" }
  }
}

provider "conohavps" {}

data "conohavps_flavor" "plan" { name = "g2l-t-c2m1" }              # Linux・時間課金・2 コア・1 GB
data "conohavps_image" "ubuntu" { name = "vmi-ubuntu-24.04-amd64" }

resource "conohavps_keypair" "me" {
  name       = "me"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "conohavps_volume" "boot" {
  name        = "web-boot"
  size        = 100
  volume_type = "c3j1-ds02-boot"
  image_ref   = data.conohavps_image.ubuntu.id
}

resource "conohavps_instance" "web" {
  instance_name_tag = "web"
  flavor_id         = data.conohavps_flavor.plan.id
  block_device      = [{ uuid = conohavps_volume.boot.id }]
  key_name          = conohavps_keypair.me.name
  security_group    = [{ name = "IPv4v6-SSH" }] # 省略すると default（同じ SG 内からの通信だけ許可）になり SSH が通らない
  power_state       = "ACTIVE"
}

output "ipv4" {
  value = [for net, addrs in conohavps_instance.web.addresses : [for a in addrs : a.addr if a.version == 4][0] if startswith(net, "ext-")][0]
}
```

```sh
terraform init
terraform plan
terraform apply
ssh root@$(terraform output -raw ipv4)
terraform destroy   # 停止中も課金されるので、使い終わったら消す
```

フレーバー名の読み方（`g2l-t-c2m1` = Linux・時間課金・2 コア・1 GB）、全リソースの文書へのリンク、料金、ConoHa 側の制約は [llms.txt](llms.txt) にまとめています。

### つまずきどころ

| 事象 | 対処 |
| --- | --- |
| 作りたてのアカウントで 2 台目のサーバーが `Number of flavors (plans) allowed per project is limit` | ConoHa 側の台数上限。引き上げは ConoHa に申請する |
| 追加 IP（`conohavps_additional_ip`）の destroy が 400 で止まる | 契約から 30 日間は解約できない。30 日後に destroy し直す。試すだけでも 1 か月分（424 円）かかる |
| `conohavps_volume_attachment` の apply が失敗する | 追加 SSD の付け外しはサーバーが `SHUTOFF` のときだけ。`power_state = "SHUTOFF"` にしてから付ける |
| 認証が 400 `The request you have made requires authentication` | 3 つの値のどれかが違う。API ページでパスワードを設定し直すのが早い |

## 要件

- [Terraform](https://developer.hashicorp.com/terraform/install) >= 1.0
- [Go](https://go.dev/doc/install) >= 1.24

## 開発

### ビルド

ビルドには Go のインストールが必要になります。

Go のインストールが確認できたら、`make build` を実行します。

```
$ make build
```
### テスト

下記の環境変数の設定が必要です。各変数の詳細については、ドキュメントを参照してください。

- `CONOHAVPS_USER_ID`
- `CONOHAVPS_PASSWORD`
- `CONOHAVPS_TENANT_ID`
- `CONOHAVPS_REGION`
- `CONOHAVPS_IDENTITY_ENDPOINT`

環境変数が設定できたら、`make testacc` を実行します。

```
$ make testacc
```

> [!CAUTION]
> テストでは、ConoHa VPS 上に実際にリソースを作成するので、料金が発生することに注意してください。

## コントリビュート

- [コントリビューションガイド](./CONTRIBUTING.md)
- [行動規範](./CODE_OF_CONDUCT.md)
- [セキュリティポリシー](./SECURITY.md)

## ライセンス

このプロジェクトは [Apache License 2.0](LICENSE) の下で公開されています。