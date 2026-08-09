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
        patch?: never;
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
        /** @description /healthz のレスポンスボディ */
        HealthResponse: {
            /** @example ok */
            status: string;
        };
        /** @description 失敗したときの本文。内部情報は載せない。原因の詳細はサーバー側のログに残す。 */
        ErrorResponse: {
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
        /** @description 一覧で返すボード。シーンは大きいので含めない */
        BoardSummary: {
            id: string;
            name: string;
            /** Format: date-time */
            createdAt: string;
            /** Format: date-time */
            updatedAt: string;
        };
        /**
         * @description シーンと作成先を含むボード。
         *
         *     作成先は一覧（BoardSummary）には含めない。`targetLocked` の算出に
         *     run の照会が要るので、ボード数だけ問い合わせが増える。
         */
        BoardDetail: components["schemas"]["BoardSummary"] & {
            /** @description Excalidraw のシーン JSON をそのまま入れた文字列 */
            scene: string;
            /** @description 作成先リポジトリの所有者。未選択なら空文字 */
            repositoryOwner: string;
            /** @description 作成先リポジトリの名前。未選択なら空文字 */
            repositoryName: string;
            /** @description draft issue を作る Projects v2 の node ID。未選択なら空文字 */
            projectId: string;
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
         */
        BoardTarget: {
            repositoryOwner: string;
            repositoryName: string;
            projectId: string;
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
        };
        /** @description ボード作成のリクエストボディ */
        CreateBoardRequest: {
            name: string;
            /** @description 省略すると空のシーンで作る */
            scene?: string;
        };
        /** @description シーン保存のリクエストボディ */
        SaveSceneRequest: {
            scene: string;
        };
        /** @description 作成済みの draft issue 1 件 */
        SyncItem: {
            /** @description GitHub Projects v2 の item ID */
            itemId: string;
            kind: components["schemas"]["ItemKind"];
            title: string;
            /** @description 解釈結果の中でだけ通じる ID。親子の対応づけに使う */
            localId: string;
            /** @description epic に属する issue のとき、その epic の localId */
            parentLocalId?: string;
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
            /** @description 前回実行で作られた draft issue。未実行なら省略する */
            items?: components["schemas"]["SyncItem"][];
        };
        /** @description 解釈結果に含まれる draft issue 1 件。まだ作成はしていない */
        InterpretedItem: {
            localId: string;
            kind: components["schemas"]["ItemKind"];
            title: string;
            body: string;
            parentLocalId?: string;
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
         * @description Host または Origin が許可されていない。ブラウザ由来の cross-site
         *     リクエストを弾いた場合（ADR 0013）。全エンドポイントで起こりうる。
         */
        Forbidden: {
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
            403: components["responses"]["Forbidden"];
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
            /** @description 保存した */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            400: components["responses"]["BadRequest"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
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
        requestBody?: never;
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
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            /** @description 解釈時点と現在のシーンの contentHash が食い違う */
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
