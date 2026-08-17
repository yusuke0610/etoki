# web/ の規約

フロントエンドを触るときの約束。全体の規約はリポジトリルートの `CLAUDE.md`。
パスはリポジトリルートからで書く。作業ディレクトリがルートのままでも辿れる
ようにするため。

## バックエンドと揃える必要がある約束

片方だけ変えると壊れるもの。理由の全体は `internal/CLAUDE.md` にある。

- **注釈の判定規則は Go 側と同じにする。** `web/src/excalidraw/annotation.ts` の
  `isAnnotation` と `internal/domain/scene.go` の `Element.isAnnotation`。規則は
  「`type === "frame"` かつ `customData.etoki` を持つ」（ルートの `CLAUDE.md`）。
- **「未保存」判定（`web/src/excalidraw/dirty.ts`）は `content_hash` とは別物。**
  保存はシーン全体を書くので、図形を動かしただけでも未保存にする。バックエンドの
  `content_hash` はテキストのみを見る。**揃えないこと。** 揃えると保存すべき
  変更を取りこぼす。
- **保存は「何の上に描いたか」を持ち回る（ADR 0020）。** `BoardDetail.updatedAt`
  を基準として保持し、保存時に `baseUpdatedAt` として送る。**応答で返る版を次の
  基準に差し替える。** 差し替え忘れると 2 回目の保存が必ず 409 になる。409 は
  失敗ではなく状態なので、**編集を捨てて読み直さない。** 揃えると、消えるのは
  相手ではなくこちらの作業になる。
- **未保存のあいだは解釈させない。** 解釈のテキストは保存済みシーンから、画像は
  画面のキャンバスから取るので、揃っていないと 1 回の解釈の入力が食い違う
  （ADR 0018）。これは UI 側だけが守っている約束で、API を直接叩けば破れる。

## 未保存のまま離れさせない（ADR 0021）

**自動保存もローカルの下書きも持たない。** 代わりに、失う直前に確認を出す。
自動で保存しないと決めた以上、知らせる責任が対になる。

- `dirty` のあいだだけ `beforeunload` を登録する（`BoardPage`）。リロード・タブを
  閉じる・戻るはアプリ側で止められない。
- キャンバスが外れる導線は `App` にある（ボードの切り替え、新しいボードの作成先
  選択、ログアウト）。**通す前に `confirmDiscard` を通す。** 導線を足したら
  ここにも足す。「作成先を変更」だけは `dirty` でボタンを止める形で揃えてある。
- **`confirmDiscard` とキャンバスを外すことのあいだに `await` を置かない。**
  置くと、待っているあいだの描き足しを確認なしで捨てる。切り替えは「取ってから
  訊く」、ログアウトは「訊いてから外して、通信はその後」。未保存かどうかを ref で
  持っているのも、待ちを挟んだ判定が古い値を見ないようにするため。
- **確認の裏で保存しない。** 捨てるつもりの試し描きまで残る。
- 未保存かどうかを決めるのは `BoardPage`。`App` は `onDirtyChange` で受け取る
  だけで、自前で判定し直さない。作り直すと「未保存」の定義が 2 箇所になる。

## ボードの一覧

- **一覧は作成先でまとめて見せる**（ADR 0019）。木は実体の包含ではなく射影。
  1 つの Project に複数のボードがぶら下がる。組み立ては
  `web/src/board/grouping.ts` の純関数にある。
- **`project_number` / `project_title` は表示用のスナップショット。** 作成先を
  選んだ時点の値で、判定には使わない。古くなったら選び直しで直す（詳細は
  `internal/CLAUDE.md`）。

## 更新と取り残しの見せ方（ADR 0026）

- **`AnnotationStatus.items` は「前回作ったもの」ではなく「いま GitHub に在る
  もの」。** サーバーが run 履歴を畳んで返す。更新は同じ `itemId` に吸収され、
  今回触らなかったものも残り続ける。
- **書き換える項目には印を出す。** 作るのか書き換えるのかは押す前に見えている
  必要がある。どちらも取り消せないが、書き換えは前の内容を消す。
- **取り残しは作成ボタンの手前に出す。** 押したあとに気づいても draft issue は
  削除できない。**消す判断はしない。** etoki にできるのは「残ります」と見せる
  ところまで。
- **選択を外すと、その項目の更新先は取り残しに戻る。** 外したことで何が起きるかを
  押す前に見せる。ADR 0024 の「親を失うことは黙って起こさない」と同じ形で、
  判定は `interpretationDraft.ts` の純関数に置く。

## 契約の型

- **`web/src/api/types.ts` の名前を import する。** `components["schemas"][...]`
  を直書きしない。**独自の別名を付けない。** 名前が食い違うと、契約を直したときに
  追随先を機械的に辿れなくなる。
- `web/src/api/generated.ts` は生成物。手で編集しない（詳細は `api/CLAUDE.md`）。

## E2E テスト

`web/e2e/` に置く。Playwright で実ブラウザを動かし、**バックエンドは起動しない。**
API はすべて `page.route` で差し替える（ADR 0012）。

- **確かめるのは画面側の約束。** 3 状態の表示、解釈するまで作成ボタンが出ない
  こと、途中失敗の見せ方、保存で解釈結果を捨てること、未保存のあいだは解釈
  できないこと、ロールと GitHub の権限による出し分け（`sharing.spec.ts`）。
  外部連携そのものはアダプタの単体テストの担当。E2E に持ち込まない。
- **注釈範囲の画像を書き出せることは E2E でしか確かめられない。** canvas が
  要るので vitest では動かない。`interpretation.spec.ts` がリクエストに画像が
  載ったことを見ている。ここを消すと、書き出しが壊れても単体テストは緑のまま。
- **モックの応答本文は生成型で書く。** `web/e2e/helpers/api.ts` の `Reply<T>`。
  型を外すと、モックだけが古い契約のまま緑になる。
- **ルートはパス名の述語でマッチさせる。** パスの途中に `api` を含むグロブは
  Vite が配信する `/src/api/types.ts` まで傍受してしまう。
- **作成先の選択画面を触るときは `.picker` に絞る**（`web/e2e/helpers/board.ts` の
  `picker` / `chooseTarget`）。サイドバーの木にも `acme/web` や
  `#1 ロードマップ` と同じ名前のボタンが並ぶので、ページ全体から名前で引くと
  2 つ見つかって落ちる（ADR 0019）。
- `make test` には含めない。速さを保ちたいので、E2E はコミット前と CI で回す。

### 報告にブラウザの実行結果を添える

**UI に触れる修正をしたら、`make test-e2e` を実行し、ブラウザの実行結果の
スクリーンショットを報告に添える。** 「動くはず」ではなく、実際にどう表示された
かを見せる。テストが緑であることと、画面が意図どおりであることは別の話で、
セレクタが通っても見た目が壊れていることはある。

- 画像は `web/e2e/screenshots.spec.ts` が `web/e2e-output/screenshots/` に
  書き出す（gitignore 済み）。`make test-e2e` を回せば毎回作り直される。
- 添えるのは変更が現れている画面。全部を貼らない。新しい画面や状態を足した
  なら `screenshots.spec.ts` に撮影を 1 つ足す。
- 意図と違う表示になっていたら、それも隠さず添えて報告する。落ちたテストの
  スクリーンショットは `web/e2e-output/results/` にある。

## 単体テストの実行

```sh
# ファイル指定 / テスト名指定
cd web && bunx vitest run src/excalidraw/annotation.test.ts
cd web && bunx vitest run -t "customData"

# E2E: ファイル指定 / テスト名指定 / ブラウザを見ながら
cd web && bunx playwright test e2e/interpretation.spec.ts
cd web && bunx playwright test -g "解釈するまで"
cd web && bunx playwright test --ui
```

これらは `make` を通らないので **devShell の中で実行する**。`make` のように
`nix develop` へ包み直されない。

## ツールチェーン上の非自明な設定

触る前に理由を把握しておくべきもの。消すと壊れる。

- **`web/vite.config.ts` の test セクション** — excalidraw を vitest で読むのに
  3 つ必要。prod バンドルへの `alias`、`open-color`（実体が JSON）の `inline`、
  そして `src/test-setup.ts` の canvas スタブ（import 時に 2D コンテキストの
  機能検出が走る）。
- **`web/vite.config.ts` の `test.include`** — vitest の既定は `*.spec.ts` も
  拾うため、明示しないと Playwright の spec を vitest が実行しようとする。
- **`web/tsconfig.json` の `include` に `e2e` がある。** E2E のモックが契約の
  生成型から外れたことを `tsc` に見つけさせるため。外すと ADR 0012 の仕組みが
  黙って効かなくなる。
- **React は 18 系に固定。** `@excalidraw/excalidraw` との組み合わせを揃えるため。
- **`web/src/excalidraw/assumptions.test.ts`** — `customData` が serialize と
  restore を越えて残るという、注釈設計の前提そのものを固定するテスト。
  ライブラリ更新で落ちたら設計を見直す合図。
- **`@playwright/test` のバージョンは exact 指定。** devShell が渡す
  `playwright-driver.browsers`（nixpkgs 側）と揃っていないと、要求される
  リビジョンのブラウザが見つからず E2E が起動しない。`nix flake update` で
  `playwright-driver` が動いたら `web/package.json` も同じ値に上げる。
