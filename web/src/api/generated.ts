/**
 * 自動生成ファイル — 手編集禁止。
 *
 * api/openapi.yaml から openapi-typescript で生成される。再生成は `make codegen`。
 * 契約の正本は openapi.yaml であり、このファイルはその機械的な写し。直接編集
 * しても次の生成で上書きされ、CI の codegen-drift ジョブが落ちる（ADR 0011）。
 */
export interface paths {
    "/healthz": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * プロセスが生きていることを返す
         * @description DB や外部サービスの疎通確認は含めない。etoki は自動で外部に触らないという
         *     方針のため、ヘルスチェックが副作用を持たないようにしている。
         */
        get: operations["getHealth"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/capabilities": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * いま使える機能を返す
         * @description LLM や GitHub を設定していなくても etoki は起動する（ADR 0008）。
         *     設定していない機能のエンドポイントは 503 を返すが、**それは押した後に
         *     しか分からない。** 押す前に「いまできないこと」を見せるための口。
         *
         *     返すのはプロセスの設定であって、利用者ごとの権限ではない。ボード単位の
         *     可否は `GET /api/boards/{id}/access`（ADR 0017）。**混ぜないこと。**
         *     片方が「できる」でもう片方が「できない」は普通に起きる。
         */
        get: operations["getCapabilities"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/auth/session": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * ログイン状態を返す
         * @description **常に 200 を返す。** 未ログインを 401 で伝えると、認証を設定していない
         *     構成の起動時にも 401 が出て、本当の失効と見分けがつかなくなる。
         *
         *     `authRequired` が false なら認証を設定していない。画面はログインを
         *     求めない（ADR 0015）。
         */
        get: operations["getSession"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/auth/login": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * 認可画面の URL を発行する
         * @description **GET ではなく POST。** state の発行は書き込みであり、GET にすると
         *     外部ページの `<img>` から叩けてしまう。POST なら Origin 検証が効く
         *     （ADR 0013 / 0015）。
         *
         *     遷移そのものはフロントが行う。サーバーからリダイレクトしないのは、
         *     fetch でのリダイレクト追跡が cross-origin で扱いにくいため。
         */
        post: operations["startLogin"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/auth/callback": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 認可の結果を受け取ってセッションを張る
         * @description 認証基盤からのトップレベル遷移。**etoki で唯一、副作用を持つ GET。**
         *     GET 以外にはできないので、`state` の照合で守る。サーバー発行・単回
         *     使用・期限つきなので、攻撃者は有効な値を用意できない（ADR 0015）。
         *
         *     画面に戻すためのリダイレクトを返す。ブラウザ以外から叩く用途は無い。
         */
        get: operations["completeLogin"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/auth/logout": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * セッションを破棄する
         * @description GitHub 側のトークンは取り消さない。同じ利用者が別の端末からも使って
         *     いる可能性があり、片方のログアウトで全部を切るのは意図に合わない。
         */
        post: operations["logout"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * ボードの一覧を返す
         * @description シーンは大きいので一覧には含めない。
         */
        get: operations["listBoards"];
        put?: never;
        /** ボードを作る */
        post: operations["createBoard"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        /** ボードをシーンごと返す */
        get: operations["getBoard"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * ボードの名前を変える
         * @description 名前だけを変える。シーンも作成先も触らない。
         *
         *     **`updatedAt` は進めない。** あれはシーンの版であり、保存の照合基準
         *     （ADR 0020）として使われている。名前を直しただけで進めると、そのボードを
         *     開いている別のメンバーの次の保存が「他の人が保存しました」で断られる。
         *     誰もシーンを触っていないのに衝突として読まれることになる。
         *
         *     変えられるのは editor 以上。作成先の変更（owner だけ）と揃えないのは、
         *     名前は取り消せない作成の行き先を決めるものではないため。
         */
        patch: operations["renameBoard"];
        trace?: never;
    };
    "/api/boards/{id}/scene": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        get?: never;
        /**
         * シーンを保存する
         * @description 3 状態の判定は保存済みシーンを基準にする。保存するまで編集中の内容は
         *     注釈の状態に反映されない。
         *
         *     保存はシーン全体を書く。ボードは共有できるので（ADR 0017）、後勝ちを
         *     許すと失われるのは「相手が触った要素」ではなく相手の作業すべてになる。
         *     `baseUpdatedAt` が現在の版と違えば 409 を返し、何も書かない（ADR 0020）。
         *
         *     シーンにはバイト数の上限がある。超えたら縮小も切り捨てもせず 413 を
         *     返す（ADR 0018 と同じ扱い）。
         */
        put: operations["saveScene"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/target": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        get?: never;
        /**
         * draft issue の作成先をボードに設定する
         * @description 利用者はリポジトリを選ぶが、保存するのはそのリポジトリに紐づく
         *     Projects v2。draft issue はリポジトリではなく Project に属するため
         *     （ADR 0014）。
         *
         *     そのボードで draft issue を 1 件でも作った後は 409 を返す。
         */
        put: operations["setBoardTarget"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/target/display": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        get?: never;
        /**
         * 作成先の表示用スナップショットだけを取り直す
         * @description `projectNumber` / `projectTitle` / `projectUrl` は作成先を選んだ時点の
         *     スナップショットで、判定には使わない（ADR 0019 / 0025）。GitHub 側で
         *     Project を改名すると古いままになるが、**作成先そのものは固定済みで
         *     変えられない**（ADR 0014）ため、選び直しでは直せなかった。この口は
         *     表示用の 3 つだけを更新する（ADR 0037）。
         *
         *     **作成先そのもの（リポジトリと projectId）は変えられない。**
         *     `projectId` は「どの作成先の表示名か」を確かめるために伴う。保存されて
         *     いるものと違えば 409（`target_mismatch`）を返す。作成先を変えるのは
         *     `PUT /api/boards/{id}/target` のほうで、固定後は通らない。
         *
         *     **etoki からは自動で取りにいかない。** 画面が押されたときに GitHub の
         *     Project 一覧を引き、その値をここへ送る（中核思想 3）。
         */
        put: operations["refreshBoardTargetDisplay"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/access": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        /**
         * そのボードで何ができるかを返す
         * @description etoki 側のロールと、GitHub 側の書き込み可否を**別々に**返す（ADR 0017）。
         *     「開けるが書けない」が普通に起きるので、1 つに畳むと画面に出せない。
         *
         *     `projectAccess` は**状態であって判定ではない。** 実際に作れるかは作成時に
         *     GitHub が返したものが正しい。これを見て作成を止めるのではなく、
         *     できない理由を先に見せるために使う。
         *
         *     ボード取得とは別の呼び出しにしてある。GitHub が未設定・不通でもボードは
         *     開ける必要がある。
         */
        get: operations["getBoardAccess"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/members": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        /**
         * ボードのメンバーを返す
         * @description メンバーなら誰でも見られる。誰と共有しているかを owner だけが知って
         *     いる状態にすると、招待された側は自分が何に呼ばれたのか分からない。
         */
        get: operations["listBoardMembers"];
        put?: never;
        /**
         * メンバーを招待する
         * @description 招待できるのは owner だけ。**招待される側にリポジトリのアクセス権は
         *     要らない**（ADR 0017）。ブレストに呼ぶ相手と、GitHub に書ける相手は
         *     同じではない。
         *
         *     指す相手は login だが、一度 etoki にログインしている必要がある。
         *     未ログインの login 宛に招待を積むと、改名で空いた login を取った別人に
         *     権限が渡る。その場合は 400 を返す。
         */
        post: operations["inviteBoardMember"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/members/{userId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /**
                 * @description etoki が発番した利用者の ID。login ではない。login は改名で変わるので、
                 *     指し先としては使えない（ADR 0015）。
                 */
                userId: components["parameters"]["UserId"];
            };
            cookie?: never;
        };
        get?: never;
        /**
         * メンバーのロールを変える
         * @description owner だけ。最後の owner は降格できない。誰も招待できず作成先も
         *     変えられないボードが残るため。
         */
        put: operations["setBoardMemberRole"];
        post?: never;
        /**
         * メンバーを外す
         * @description owner だけ。最後の owner は外せない。
         */
        delete: operations["removeBoardMember"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/github/repositories": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 作成先に選べるリポジトリの一覧を返す
         * @description アーカイブ済みは含めない。トークンに repo の read が無いと 0 件になるが、
         *     権限不足と「本当に 1 つも無い」は区別できない。
         */
        get: operations["listRepositories"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/github/repositories/{owner}/{repo}/projects": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description リポジトリ所有者の login */
                owner: components["parameters"]["RepositoryOwner"];
                /** @description リポジトリ名 */
                repo: components["parameters"]["RepositoryName"];
            };
            cookie?: never;
        };
        /**
         * リポジトリに紐づく Projects v2 の一覧を返す
         * @description 閉じた Project は含めない。
         */
        get: operations["listRepositoryProjects"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/annotations": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        /**
         * 注釈の 3 状態を返す
         * @description 保存済みシーンから注釈を取り出し、最新の run の content_hash と
         *     突き合わせて uncreated / created / changed を決める。
         */
        get: operations["listAnnotations"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/annotations/{annotationId}/runs": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /** @description 注釈にした frame の要素 ID */
                annotationId: components["parameters"]["AnnotationId"];
            };
            cookie?: never;
        };
        /**
         * その注釈の実行履歴を返す
         * @description 再実行しても過去の run は消さない（ADR 0007）。**残す理由は追跡なので、
         *     読む口が無いと残している意味が無い。** GitHub 側に何世代ぶんの draft
         *     issue が在るのかは、ここでしか辿れない。
         *
         *     並びは新しい順。**新しさは `createdAt` ではなく `id` で決める。** 時刻は
         *     呼び出し側が与えるので、同じ時刻の run がありうる。
         *
         *     返すのは直近のぶんだけで、件数の上限はサーバーが決める。範囲指定は
         *     持たない（数え上げではなく「直前に何をしたか」を辿るための口なので、
         *     遡り続ける導線を作る理由が無い）。
         *
         *     **`AnnotationStatus.items` とは別物。** あちらは run 履歴を itemId で
         *     畳んだ「いま GitHub に在るもの」で、こちらは畳む前の 1 回ずつの記録
         *     （ADR 0026）。
         */
        get: operations["listAnnotationRuns"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/annotations/{annotationId}/interpret": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /** @description 注釈にした frame の要素 ID */
                annotationId: components["parameters"]["AnnotationId"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * 注釈を LLM に解釈させる
         * @description GitHub には何も作らず、sync_runs にも書かない。何を作るかは結果を見た
         *     開発者が別途トリガーする（中核思想 3）。
         *
         *     テキストは保存済みシーンから取る。`image` はフロントが書き出した注釈
         *     範囲の画像で、矢印やグルーピングのようにテキストに現れない構造を渡す
         *     ためのもの（ADR 0018）。ボディごと省略できる。
         */
        post: operations["interpretAnnotation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/boards/{id}/annotations/{annotationId}/items": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /** @description 注釈にした frame の要素 ID */
                annotationId: components["parameters"]["AnnotationId"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * 解釈結果から draft issue を作る
         * @description リクエストボディは開発者が確認した解釈結果そのもの。サーバー側で解釈し
         *     直さない。ただし内容は信用せず、ユースケース層で検証し直す。
         *
         *     `contentHash` は解釈時点の保存済みシーンのもの。現在のシーンと食い違うと
         *     409 を返す（ADR 0010）。
         */
        post: operations["createItems"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        /** @description ログイン中の利用者 */
        AuthUser: {
            /** @description 認証基盤の識別子。"github" など */
            provider: string;
            login: string;
            displayName: string;
        };
        /**
         * @description いま使える機能。**プロセスの設定であって、利用者ごとの権限ではない。**
         *
         *     false のものは押す前に理由を出すために使う。理由の文言は `ErrorCode` の
         *     `*_not_configured` と同じものを引く。そのエンドポイントを叩けば同じ
         *     原因で 503 が返るので、**先に見せる文言と後から返る理由を別に持たない。**
         */
        Capabilities: {
            /** @description 注釈を解釈できるか。false は LLM が未設定（ADR 0008） */
            interpretation: boolean;
            /**
             * @description draft issue を作れるか。false は GitHub が未設定。**作成先の候補も
             *     引けない**ので、新しいボードも作れない（ADR 0017）
             */
            creation: boolean;
            /**
             * @description ボードを共有できるか。false は認証が未設定。招待は「誰であるか」が
             *     決まって初めて意味を持つ（ADR 0016 / 0017）
             */
            sharing: boolean;
        };
        /** @description ログイン状態。認証を設定していない構成でも 200 で返る。 */
        SessionStatus: {
            /** @description 認証を設定しているか。false なら画面はログインを求めない */
            authRequired: boolean;
            authenticated: boolean;
            user?: components["schemas"]["AuthUser"];
        };
        /** @description 認可画面へ送り出すための URL */
        LoginResponse: {
            authorizeUrl: string;
        };
        /** @description /healthz のレスポンスボディ */
        HealthResponse: {
            /** @example ok */
            status: string;
        };
        /**
         * @description 失敗の原因を表す機械可読な符号。**画面はこれで打ち手を分ける。**
         *
         *     ステータスだけでは打ち手が決まらない。409 には「開き直す」「諦める」
         *     「解釈からやり直す」「入力を変える」が同居していて、利用者がすべきことは
         *     すべて違う。403 も etoki のロール不足（`forbidden_role`）と GitHub 側の
         *     拒否（`forbidden_project`）で直す場所が違い、2 層に分けて持つと決めた
         *     （ADR 0017）のに、文言に畳むと画面でその区別が消える。
         *
         *     **`*_not_configured` を 1 つに畳まない。** 設定するものが違うので、
         *     畳むと画面が「何を設定すればよいか」を言えなくなる。
         * @enum {string}
         */
        ErrorCode: "invalid_input" | "login_required" | "forbidden_role" | "forbidden_project" | "cross_site_rejected" | "not_found" | "scene_conflict" | "scene_too_large" | "target_locked" | "target_mismatch" | "content_hash_mismatch" | "previous_item_unknown" | "already_member" | "last_owner" | "target_not_selected" | "project_field_missing" | "llm_unavailable" | "interpretation_failed" | "creation_incomplete" | "github_unavailable" | "internal" | "llm_not_configured" | "github_not_configured" | "auth_not_configured" | "sharing_not_configured";
        /** @description 失敗したときの本文。打ち手は `code` で分け、`error` は手掛かりに留める。 */
        ErrorResponse: {
            code: components["schemas"]["ErrorCode"];
            /**
             * @description 原因の手掛かり。**利用者向けの文言ではない。** 画面は既定で畳む。
             *     GitHub や LLM が返した本文が実際の手掛かりになる経路があるので残す
             */
            error: string;
        };
        /**
         * @description 注釈の 3 状態。保存済みシーンの content_hash と最新 run のそれを
         *     突き合わせて決まる。
         * @enum {string}
         */
        SyncState: "uncreated" | "created" | "changed";
        /**
         * @description 注釈の粒度。空文字は「指定なし」で、粒度の判断を LLM に任せる。
         * @enum {string}
         */
        Granularity: "" | "epic" | "issue";
        /**
         * @description GitHub に作る draft issue の種別。作るのは epic と issue の 2 階層のみ
         *     （ADR 0006）。
         * @enum {string}
         */
        ItemKind: "epic" | "issue";
        /**
         * @description ボードに対する権限の強さ（ADR 0017）。
         *
         *     - `owner` … 招待とロール変更、作成先の変更ができる
         *     - `editor` … ブレストと解釈と draft issue の作成ができる。作成できるかを
         *       最終的に決めるのは GitHub
         *     - `viewer` … 読むだけ。解釈も許さない。解釈は LLM を叩く外部呼び出しで
         *       あり、閲覧者に許すのは「閲覧」ではない
         * @enum {string}
         */
        BoardRole: "owner" | "editor" | "viewer";
        /**
         * @description 作成先の Project に書けるかどうかの、いまの状態。
         *
         *     - `allowed` … 書ける
         *     - `denied` … 書けない。招待されただけで、リポジトリのアクセス権を
         *       持たない利用者がこれになる
         *     - `unknown` … 確かめられなかった。GitHub が未設定、作成先が未選択、
         *       問い合わせに失敗した、のどれか。**allowed / denied のどちらにも
         *       倒さない。** 倒すと、確かめていないことを確かめたように見せることになる
         * @enum {string}
         */
        ProjectAccess: "allowed" | "denied" | "unknown";
        /** @description そのボードで何ができるか。etoki 側と GitHub 側を別々に返す */
        BoardAccess: {
            role: components["schemas"]["BoardRole"];
            projectAccess: components["schemas"]["ProjectAccess"];
        };
        /** @description ボードのメンバー 1 人 */
        BoardMember: {
            /** @description etoki が発番した ID。指し先にはこれを使う */
            userId: string;
            /**
             * @description 認証基盤上のログイン名。表示用。移行前のボードなど、利用者を
             *     引けない場合は空文字になる
             */
            login: string;
            displayName: string;
            role: components["schemas"]["BoardRole"];
            /** Format: date-time */
            createdAt: string;
        };
        /** @description 招待のリクエストボディ */
        InviteMemberRequest: {
            /** @description 招待する相手の login。一度 etoki にログインしている必要がある */
            login: string;
            role: components["schemas"]["BoardRole"];
        };
        /** @description ロール変更のリクエストボディ */
        SetMemberRoleRequest: {
            role: components["schemas"]["BoardRole"];
        };
        /**
         * @description 一覧で返すボード。シーンは大きいので含めない。
         *
         *     作成先は含める。一覧をリポジトリと Project でまとめて見せるため
         *     （ADR 0019）。含めないのは `targetLocked` だけで、これは算出に run の
         *     照会が要り、ボード数だけ問い合わせが増える。
         */
        BoardSummary: {
            id: string;
            name: string;
            role: components["schemas"]["BoardRole"];
            /** Format: date-time */
            createdAt: string;
            /** Format: date-time */
            updatedAt: string;
            /** @description 作成先リポジトリの所有者。未選択なら空文字 */
            repositoryOwner: string;
            /** @description 作成先リポジトリの名前。未選択なら空文字 */
            repositoryName: string;
            /** @description draft issue を作る Projects v2 の node ID。未選択なら空文字 */
            projectId: string;
            /**
             * @description 作成先 Project の番号。作成先を選んだ時点のスナップショット
             *     （ADR 0019）。取得していなければ 0
             */
            projectNumber: number;
            /**
             * @description 作成先 Project の名前。作成先を選んだ時点のスナップショット
             *     （ADR 0019）。取得していなければ空文字
             */
            projectTitle: string;
            /**
             * @description 作成先 Project の URL。作成先を選んだ時点のスナップショット
             *     （ADR 0025）。取得していなければ空文字。
             *
             *     番号から組み立てたものではなく GitHub が返したもの。Projects v2 の
             *     URL は owner が user か org かで形が変わり、etoki はどちらなのかを
             *     知らない
             */
            projectUrl: string;
        };
        /** @description シーンと作成先の固定状態を加えたボード */
        BoardDetail: components["schemas"]["BoardSummary"] & {
            /** @description Excalidraw のシーン JSON をそのまま入れた文字列 */
            scene: string;
            /**
             * @description 作成先を変更できないことを表す。そのボードで draft issue を
             *     1 件でも作ると立つ（ADR 0014）。フロントは sync_runs を
             *     数えられないので、状態としてサーバーが返す
             */
            targetLocked: boolean;
        };
        /**
         * @description draft issue の作成先。設定のリクエストボディ。
         *
         *     リポジトリと Project の両方を持つ。保存先として効くのは projectId
         *     だが、どのリポジトリから選んだかを画面に出すために owner / name も残す。
         *
         *     projectNumber と projectTitle と projectUrl は**表示用のスナップショット**
         *     であり、作成先そのものではない（ADR 0019 / 0025）。未選択の判定にも、
         *     固定済みかどうかの判定にも使わない。選ばせた画面が見せていたものを
         *     そのまま送る。
         *
         *     表示用の 3 つは任意。省略すると「知らない」（0 と空文字）として保存する。
         *     必須にすると、画面を通さない呼び出し側が値をでっち上げることになる。
         */
        BoardTarget: {
            repositoryOwner: string;
            repositoryName: string;
            projectId: string;
            projectNumber?: number;
            projectTitle?: string;
            projectUrl?: string;
        };
        /**
         * @description 作成先の表示用スナップショット。取り直しのリクエストボディ（ADR 0037）。
         *
         *     `projectId` は**変更先ではなく照合材料**。どの作成先の表示名なのかを
         *     示すために伴い、保存されているものと違えば 409 になる。リポジトリを
         *     持たないのは、この口で作成先そのものを動かせないことを形で示すため。
         *
         *     表示用の 3 つは任意。省略すると「知らない」（0 と空文字）として保存
         *     する。BoardTarget と同じ扱いにしてあるので、GitHub が URL を返さない
         *     Project でも取り直せる。
         */
        BoardTargetDisplay: {
            /** @description いま保存されている作成先の Projects v2 node ID */
            projectId: string;
            projectNumber?: number;
            projectTitle?: string;
            projectUrl?: string;
        };
        /** @description 作成先を選ぶときに見せるリポジトリ */
        Repository: {
            owner: string;
            name: string;
            description?: string;
        };
        /** @description リポジトリに紐づく Projects v2 */
        Project: {
            /** @description GraphQL の node ID。作成先として保存するのはこれ */
            id: string;
            /** @description リポジトリ内での番号。GitHub の URL に出る */
            number: number;
            title: string;
            /**
             * @description Project のページ。**番号から組み立てず GitHub が返したものを運ぶ。**
             *     Projects v2 の URL は owner が user か org かで形が変わり、etoki は
             *     どちらなのかを知らない（ADR 0025）
             */
            url: string;
        };
        /**
         * @description ボード作成のリクエストボディ。
         *
         *     **作成先は必須。** 候補は `minPermissionLevel: WRITE` で絞ってあるので
         *     （ADR 0014）、書ける Project を 1 つも持たない人はボードを作れない。
         *     「ボードの作成にはリポジトリへのアクセス権が要る」はこれで満ちる
         *     （ADR 0017）。
         *
         *     副産物として、作成先が未選択のボードは新規には生まれなくなる。移行前の
         *     ボードは未選択のまま残るので、`projectId` が空文字の経路は消えない。
         */
        CreateBoardRequest: {
            name: string;
            repositoryOwner: string;
            repositoryName: string;
            /** @description draft issue を作る Projects v2 の node ID */
            projectId: string;
            /**
             * @description 作成先 Project の番号。表示用のスナップショット（ADR 0019）。
             *     省略すると 0（名前を知らない）で保存する
             */
            projectNumber?: number;
            /**
             * @description 作成先 Project の名前。表示用のスナップショット（ADR 0019）。
             *     省略すると空文字（名前を知らない）で保存する
             */
            projectTitle?: string;
            /**
             * @description 作成先 Project の URL。表示用のスナップショット（ADR 0025）。
             *     省略すると空文字（URL を知らない）で保存する
             */
            projectUrl?: string;
            /** @description 省略すると空のシーンで作る */
            scene?: string;
        };
        /**
         * @description 改名のリクエストボディ。
         *
         *     名前だけを持つ。作成先やシーンを一緒に送れる形にすると、この経路でも
         *     作成先を書けることになり、固定（ADR 0014）が意味を失う。
         */
        RenameBoardRequest: {
            /** @description 新しい名前。空文字と空白だけは弾く */
            name: string;
        };
        /**
         * @description シーン保存のリクエストボディ。
         *
         *     `baseUpdatedAt` は任意にしない。任意にすると API を直接叩く経路で照合を
         *     素通りでき、防ぎたい後勝ちがそのまま残る（ADR 0010 と同じ理由）。
         */
        SaveSceneRequest: {
            scene: string;
            /**
             * Format: date-time
             * @description 編集の基準にしたボードの `updatedAt`。取得時に返ったものをそのまま
             *     送り返す。現在の版と違えば 409 になり、シーンは書き換わらない
             *     （ADR 0020）
             */
            baseUpdatedAt: string;
        };
        /**
         * @description 保存後のボードの版。
         *
         *     返さないと、クライアントは保存のたびにボードを取り直さないと次の保存が
         *     できない。取り直すとシーンまで運ぶことになる。
         */
        SaveSceneResponse: {
            /**
             * Format: date-time
             * @description 保存後の `updatedAt`。次の保存の `baseUpdatedAt` になる
             */
            updatedAt: string;
        };
        /**
         * @description 1 つの run がその item に対して何をしたか（ADR 0026）。
         *
         *     `created` は新しく作った、`updated` は既存の draft issue を書き換えた。
         *     記録していなかった頃の run はすべて `created`。当時は更新の経路が無かった。
         * @enum {string}
         */
        SyncAction: "created" | "updated";
        /** @description 作成済みの draft issue 1 件 */
        SyncItem: {
            /** @description GitHub Projects v2 の item ID */
            itemId: string;
            kind: components["schemas"]["ItemKind"];
            title: string;
            /** @description 作成時の本文。記録していなかった頃の run では空 */
            body: string;
            /** @description 解釈結果の中でだけ通じる ID。親子の対応づけに使う */
            localId: string;
            /** @description epic に属する issue のとき、その epic の localId */
            parentLocalId?: string;
            action: components["schemas"]["SyncAction"];
        };
        /**
         * @description 1 つの注釈に対する 1 回ぶんの実行の記録（ADR 0007）。
         *
         *     **「そのときの全体像」ではなく「その 1 回で何をしたか」**（ADR 0026）。
         *     触らなかった item はここには現れない。いま GitHub に在るものが知りたい
         *     なら `AnnotationStatus.items`（畳み込み）を見る。
         */
        SyncRun: {
            /**
             * Format: int64
             * @description run の識別子。**新しさの順はこれで決まる**（時刻は呼び出し側が
             *     与えるので、同じ値の run がありうる）
             */
            id: number;
            /**
             * Format: date-time
             * @description 実行の時刻
             */
            createdAt: string;
            /**
             * @description その run で作成または更新した draft issue。**空配列がありうる。**
             *     1 件も作れずに終わった run も記録として残る（ADR 0009）
             */
            items: components["schemas"]["SyncItem"][];
        };
        /** @description 注釈 1 つの状態 */
        AnnotationStatus: {
            id: string;
            name: string;
            granularity: components["schemas"]["Granularity"];
            state: components["schemas"]["SyncState"];
            /**
             * Format: date-time
             * @description 前回実行の時刻。未実行なら省略する
             */
            lastSyncedAt?: string;
            /**
             * @description この注釈が GitHub に在らしめている draft issue（ADR 0026）。
             *
             *     **前回実行で作られたものではなく、run 履歴を itemId で畳んだもの。**
             *     更新は同じ itemId に吸収され、今回触らなかったものも残り続ける。
             *     最新 run だけを返すと、更新のあとに取り残しが画面から消える。
             *     1 件も無ければ省略する
             */
            items?: components["schemas"]["SyncItem"][];
        };
        /** @description 解釈結果に含まれる draft issue 1 件。まだ作成はしていない */
        InterpretedItem: {
            localId: string;
            kind: components["schemas"]["ItemKind"];
            title: string;
            body: string;
            parentLocalId?: string;
            /**
             * @description 書き換える対象の itemId（ADR 0026）。新しく作るなら省略する。
             *
             *     解釈のレスポンスでは LLM が対応づけた候補が入る。作成のリクエストでは
             *     開発者が確かめた結果として送り返す。**サーバーはこの値がその注釈の
             *     ものであることを確かめる。** 確かめずに通すと、任意の node ID を
             *     書いて無関係な draft issue を書き換えられる
             */
            previousItemId?: string;
        };
        /**
         * @description 注釈の frame 範囲だけを写した画像。frame の外にある要素は含めない。
         *
         *     サーバーは保存しない。解釈 1 回のあいだ LLM へ渡すためだけに使う
         *     （ADR 0018）。
         */
        AnnotationImage: {
            /**
             * @description 画像の MIME タイプ。PNG だけを受け付ける
             * @enum {string}
             */
            mediaType: "image/png";
            /**
             * Format: byte
             * @description 画像のバイト列を base64 で表したもの。デコード後のバイト数に上限が
             *     あり、超えると 400 を返す。上限は超えても縮小せず弾く。黙って劣化
             *     させると、渡したはずの情報が消えたことに気づけない（ADR 0018）
             */
            data: string;
        };
        /**
         * @description 解釈のリクエストボディ。テキストは保存済みシーンから取るので、ここには
         *     シーンから作れないものだけを載せる。
         */
        InterpretRequest: {
            image?: components["schemas"]["AnnotationImage"];
        };
        /**
         * @description LLM が注釈をどう解釈したか。
         *
         *     `summary` は GitHub には作らない。作成前に「こう読んだ」を見せるためだけに
         *     使う（ADR 0006）。
         *
         *     作成のリクエストボディとしてもこの形をそのまま使う。開発者が確認した
         *     結果を送り返す、という流れなので別の型にしない。
         */
        Interpretation: {
            summary: string;
            /**
             * @description 解釈の入力になった保存済みシーンのハッシュ。フロントでは組み立てず、
             *     受け取ったものをそのまま送り返す
             */
            contentHash: string;
            items: components["schemas"]["InterpretedItem"][];
        };
        /** @description 作成した run。途中で失敗しても、作れたぶんは items に入る */
        CreatedRun: {
            /** Format: int64 */
            runId: number;
            /** Format: date-time */
            createdAt: string;
            items: components["schemas"]["SyncItem"][];
            /** @description 途中で失敗したことを表す */
            incomplete?: boolean;
            /** @description 途中で失敗した理由 */
            error?: string;
        };
    };
    responses: {
        /** @description リクエストの内容が不正 */
        BadRequest: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
        /**
         * @description ログインしていない、またはセッションが失効した。認証を設定している
         *     場合にだけ起こる（ADR 0015）。
         */
        Unauthorized: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
        /**
         * @description 次のどれか。
         *
         *     - Host または Origin が許可されていない。ブラウザ由来の cross-site
         *       リクエストを弾いた場合（ADR 0013）。全エンドポイントで起こりうる
         *     - ボードのメンバーではあるが、その操作にロールが足りない（ADR 0017）。
         *       **メンバーでない場合は 404。** 区別すると ID を総当たりして他人の
         *       ボードの存在を確かめられる
         *     - GitHub がその Project への書き込みを拒んだ。etoki は実行者の
         *       トークンで叩くので、リポジトリのアクセス権が無ければここに来る
         */
        Forbidden: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
        /**
         * @description シーンが保存できる大きさを超えている。**縮小も切り捨てもせずに弾く**
         *     （ADR 0018 と同じ扱い）。効いてくるのはキャンバスに貼った画像で、
         *     シーンには base64 で丸ごと乗る。
         *
         *     400 ではないのは、中身の誤りではなく大きさだから。打ち手が「送った
         *     内容を直す」ではなく「貼った画像を減らす」になる。
         */
        SceneTooLarge: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
        /**
         * @description その機能に必要な設定がされていない。URL の誤りではないので 404 に
         *     しない。
         */
        NotConfigured: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
        /** @description 対象が見つからない */
        NotFound: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
        /** @description サーバー側の想定外の失敗 */
        InternalError: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
    };
    parameters: {
        /** @description ボードの ID */
        BoardId: string;
        /** @description 注釈にした frame の要素 ID */
        AnnotationId: string;
        /**
         * @description etoki が発番した利用者の ID。login ではない。login は改名で変わるので、
         *     指し先としては使えない（ADR 0015）。
         */
        UserId: string;
        /** @description リポジトリ所有者の login */
        RepositoryOwner: string;
        /** @description リポジトリ名 */
        RepositoryName: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getHealth: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description プロセスは生きている */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HealthResponse"];
                };
            };
            403: components["responses"]["Forbidden"];
        };
    };
    getCapabilities: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description いま使える機能 */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Capabilities"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
        };
    };
    getSession: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description ログイン状態 */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionStatus"];
                };
            };
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
        };
    };
    startLogin: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description 送り出す先 */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LoginResponse"];
                };
            };
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
            /** @description 認証が設定されていない */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    completeLogin: {
        parameters: {
            query: {
                code: string;
                state: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /**
             * @description 画面へ戻す。成功時だけでなく、state が古い・code を使い切った
             *     場合もここに来る。ブラウザのトップレベル遷移に JSON を返すと
             *     利用者が生のエラー本文を見ることになるため
             */
            302: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description code か state が付いていない（直接叩かれた場合） */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
            /** @description 認証が設定されていない */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    logout: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description 破棄した。もともとログインしていなくても 204 */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
        };
    };
    listBoards: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description ボードの一覧。0 件でも配列を返す */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardSummary"][];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
        };
    };
    createBoard: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CreateBoardRequest"];
            };
        };
        responses: {
            /** @description 作成したボード */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardDetail"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            413: components["responses"]["SceneTooLarge"];
            500: components["responses"]["InternalError"];
        };
    };
    getBoard: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description ボード */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardDetail"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            500: components["responses"]["InternalError"];
        };
    };
    renameBoard: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RenameBoardRequest"];
            };
        };
        responses: {
            /** @description 改名後のボード */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardDetail"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            500: components["responses"]["InternalError"];
        };
    };
    saveScene: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SaveSceneRequest"];
            };
        };
        responses: {
            /** @description 保存した。次の保存の基準になる版を返す */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SaveSceneResponse"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /** @description 基準にした版が古い。他の誰かがすでに保存している */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            413: components["responses"]["SceneTooLarge"];
            500: components["responses"]["InternalError"];
        };
    };
    setBoardTarget: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["BoardTarget"];
            };
        };
        responses: {
            /** @description 設定後のボード */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardDetail"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /** @description すでに draft issue を作っているので作成先を変えられない */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            500: components["responses"]["InternalError"];
        };
    };
    refreshBoardTargetDisplay: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["BoardTargetDisplay"];
            };
        };
        responses: {
            /** @description 更新後のボード */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardDetail"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /** @description 送られた projectId が保存されている作成先と違う */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description そのボードには作成先が設定されていない */
            422: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            500: components["responses"]["InternalError"];
        };
    };
    getBoardAccess: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description 権限 */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardAccess"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["NotConfigured"];
        };
    };
    listBoardMembers: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description メンバー */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardMember"][];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["NotConfigured"];
        };
    };
    inviteBoardMember: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["InviteMemberRequest"];
            };
        };
        responses: {
            /** @description 招待した */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardMember"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /** @description すでにメンバー */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            500: components["responses"]["InternalError"];
            503: components["responses"]["NotConfigured"];
        };
    };
    setBoardMemberRole: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /**
                 * @description etoki が発番した利用者の ID。login ではない。login は改名で変わるので、
                 *     指し先としては使えない（ADR 0015）。
                 */
                userId: components["parameters"]["UserId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SetMemberRoleRequest"];
            };
        };
        responses: {
            /** @description 変更後のメンバー */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BoardMember"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /** @description 最後の owner は降格できない */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            500: components["responses"]["InternalError"];
            503: components["responses"]["NotConfigured"];
        };
    };
    removeBoardMember: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /**
                 * @description etoki が発番した利用者の ID。login ではない。login は改名で変わるので、
                 *     指し先としては使えない（ADR 0015）。
                 */
                userId: components["parameters"]["UserId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description 外した */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /** @description 最後の owner は外せない */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            500: components["responses"]["InternalError"];
            503: components["responses"]["NotConfigured"];
        };
    };
    listRepositories: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description リポジトリの一覧。0 件でも配列を返す */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Repository"][];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
            /** @description GitHub の呼び出しに失敗した */
            502: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description GitHub が設定されていない */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listRepositoryProjects: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description リポジトリ所有者の login */
                owner: components["parameters"]["RepositoryOwner"];
                /** @description リポジトリ名 */
                repo: components["parameters"]["RepositoryName"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Projects v2 の一覧。0 件でも配列を返す */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Project"][];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            500: components["responses"]["InternalError"];
            /** @description GitHub の呼び出しに失敗した */
            502: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description GitHub が設定されていない */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listAnnotations: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description 注釈の状態。0 件でも配列を返す */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AnnotationStatus"][];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            500: components["responses"]["InternalError"];
        };
    };
    listAnnotationRuns: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /** @description 注釈にした frame の要素 ID */
                annotationId: components["parameters"]["AnnotationId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description 実行の履歴。新しい順。一度も実行していなければ空配列 */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SyncRun"][];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            500: components["responses"]["InternalError"];
        };
    };
    interpretAnnotation: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /** @description 注釈にした frame の要素 ID */
                annotationId: components["parameters"]["AnnotationId"];
            };
            cookie?: never;
        };
        /** @description 省略すると、これまでどおりテキストだけで解釈する。 */
        requestBody?: {
            content: {
                "application/json": components["schemas"]["InterpretRequest"];
            };
        };
        responses: {
            /** @description 解釈の結果 */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Interpretation"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            500: components["responses"]["InternalError"];
            /** @description LLM の呼び出しに失敗した、または出力がスキーマを満たさなかった */
            502: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description LLM が設定されていない */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createItems: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description ボードの ID */
                id: components["parameters"]["BoardId"];
                /** @description 注釈にした frame の要素 ID */
                annotationId: components["parameters"]["AnnotationId"];
            };
            cookie?: never;
        };
        /** @description 解釈のエンドポイントが返したものをそのまま送り返す */
        requestBody: {
            content: {
                "application/json": components["schemas"]["Interpretation"];
            };
        };
        responses: {
            /**
             * @description 作成した run。途中で失敗した場合も、作れたぶんは items に入り
             *     `incomplete` が立つ（ADR 0009）。
             */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CreatedRun"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /**
             * @description 解釈時点と現在のシーンの contentHash が食い違うか、`previousItemId` が
             *     その注釈のものではない。どちらも解釈からやり直す
             */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /**
             * @description ボードに作成先が設定されていない、または Projects v2 側に必要な
             *     フィールドが無い
             */
            422: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            500: components["responses"]["InternalError"];
            /** @description GitHub への作成が 1 件も成功しなかった */
            502: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description GitHub が設定されていない */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
}
