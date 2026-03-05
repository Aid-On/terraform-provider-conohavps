<div align="center">
  <img src="./assets/ConoHaVPS_logo.png" title="ConoHa VPS" height="150" />
</div>

# Terraform ConoHa VPS Provider

- Terraform ウェブサイト: https://developer.hashicorp.com/terraform
- ドキュメント: https://registry.terraform.io/providers/gmo-internet/conohavps/latest/docs

Terraform ConoHa VPS Provider は、Terraform が [ConoHa VPS](https://vps.conoha.jp/) 上のリソースを管理できるようにするプラグインです。

現在、下記リソースの管理に対応しています。

- サーバー
- SSHキーペア
- ボリューム
- セキュリティグループ
- セキュリティグループルール

詳細については、ドキュメントを参照してください。

> [!IMPORTANT]
> Terraform ConoHa VPS Provider は、APIユーザーの認証情報を使用します。APIユーザーの作成については、[APIユーザーを作成する](https://doc.conoha.jp/reference/api-vps3/api-cp-vps3/cp-create_api_user-v3/) を参照してください。

> [!WARNING]
>  本ソフトウェアは現在ベータ版です。機能や動作が予告なく変更される場合があります。本番環境での使用前には十分なテストを行ってください。このベータ版 Terraform ConoHa VPS Provider を使用することで、これらの条件に同意したものとみなされます。

## 使用例

ドキュメントを参照してください。

> [!IMPORTANT]
> 本ソフトウェアは現在ベータ版につきデータソースは提供しておりません。リソースの作成に必要なパラメータについては [公開API(ConoHa VPS Ver.3.0)](https://doc.conoha.jp/reference/api-vps3) より取得ください。

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