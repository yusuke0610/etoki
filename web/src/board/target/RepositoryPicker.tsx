import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { githubApi } from "../../api/boards";
import {
  describeFailure,
  PROJECT_LIST_MESSAGES,
  type Failure,
} from "../../api/errorMessage";
import type { BoardTarget, Project, Repository } from "../../api/types";
import { ErrorNotice } from "../../app/ErrorNotice";
import { log } from "../../app/logger";
import { projectFieldsNotice, type FieldsCheckState } from "./projectFieldsNotice";

type Props = {
  /** 見出しに出す名前。作成前のボードにはまだ ID が無いので名前だけ受ける。 */
  title: string;
  /**
   * 作成先が決まったときに呼ぶ。
   *
   * **保存はここではしない。** 新規作成ではボードごと作り、既存ボードでは
   * 作成先だけを差し替える。どちらなのかを知っているのは呼び出し側だけ。
   * 失敗は例外で返してよい。この画面がそのまま表示する。
   */
  onSelected: (target: BoardTarget) => Promise<void>;
  /** 選び直しをやめる。引き返す先が無い場面では渡さない。 */
  onCancel?: () => void;
};

/**
 * ブレストを始める前に、draft issue の作成先を選ばせる。
 *
 * 利用者が選ぶのはリポジトリだが、保存するのはそこに紐づく Projects v2。
 * draft issue はリポジトリではなく Project に属するため、2 段になる（ADR 0014）。
 *
 * どれかを既定で選んだり、1 件しか無いときに自動で確定したりしない。
 * 作成先は取り返しのつかない選択なので、開発者に選ばせる（中核思想 3）。
 *
 * **Project は押して選び、確定は別のボタンで行う**（ADR 0088）。選んだときに
 * 作成に要るフィールドが揃っているかを 1 度だけ確かめて見せる。押した瞬間に
 * 確定すると、確かめた結果を見せる前に画面が切り替わる。**結果で確定を止めない。**
 * 判定は作成のときに GitHub が返したもので決まる。
 */
export function RepositoryPicker({ title, onSelected, onCancel }: Props) {
  const [repositories, setRepositories] = useState<Repository[] | null>(null);
  // 候補を取り切らずに辿るのをやめたか（ADR 0054）。
  const [truncated, setTruncated] = useState(false);
  const [repository, setRepository] = useState<Repository | null>(null);
  const [projects, setProjects] = useState<Project[] | null>(null);
  // Project の一覧を取れなかったか。取れないまま「読み込み中…」を出し続けると、
  // 上に出した失敗の案内と食い違う（#293）。
  const [projectsFailed, setProjectsFailed] = useState(false);
  // 選んだがまだ確定していない Project と、そのフィールドを確かめた結果
  // （null は確かめている最中）。
  const [chosen, setChosen] = useState<Project | null>(null);
  const [fieldsCheck, setFieldsCheck] = useState<FieldsCheckState>(null);
  const [error, setError] = useState<Failure | null>(null);
  const [saving, setSaving] = useState(false);
  const [filter, setFilter] = useState("");

  useEffect(() => {
    let cancelled = false;

    void (async () => {
      try {
        const list = await githubApi.repositories();
        if (cancelled) return;
        setRepositories(list.repositories);
        setTruncated(list.truncated);
      } catch (e) {
        if (!cancelled) setError(describeFailure("リポジトリを取得できませんでした", e));
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  /**
   * 絞り込みは手元で行う（ADR 0054）。
   *
   * **サーバーに問い合わせ直さない。** 打ち切りの外は絞り込んでも出てこないので、
   * 問い合わせ直しても答えは変わらない。絞れるのは取れたぶんまで、という事実は
   * 打ち切りの知らせのほうが持つ。
   *
   * 見るのは `owner/name` だけ。説明文まで対象にすると、打った語がどこに
   * 当たったのかが一覧から読めない。
   */
  const visible = useMemo(() => {
    if (repositories === null) return null;

    const needle = filter.trim().toLowerCase();
    if (needle === "") return repositories;

    return repositories.filter((r) =>
      `${r.owner}/${r.name}`.toLowerCase().includes(needle),
    );
  }, [filter, repositories]);

  // リポジトリを続けて押すと応答が前後しうる。番号を振って最後に投げたものだけ
  // 反映する。古い応答で上書きすると、選んでいないリポジトリのプロジェクトが
  // 並ぶ。作成先を取り違える経路なので塞いでおく。
  const projectsRequest = useRef(0);

  const chooseRepository = useCallback(async (repo: Repository) => {
    const request = ++projectsRequest.current;

    setRepository(repo);
    setProjects(null);
    setProjectsFailed(false);
    setChosen(null);
    setError(null);

    try {
      const list = await githubApi.projects(repo.owner, repo.name);
      if (request !== projectsRequest.current) return;
      setProjects(list);
    } catch (e) {
      if (request !== projectsRequest.current) return;
      setProjectsFailed(true);
      // 拒まれた理由はリポジトリの権限とは限らない。この場面でだけ文を
      // 差し替える（`PROJECT_LIST_MESSAGES`、#293）。
      setError(
        describeFailure("プロジェクトを取得できませんでした", e, PROJECT_LIST_MESSAGES),
      );
    }
  }, []);

  // Project を続けて押すと応答が前後しうる。**応答が名乗る projectId と、
  // 最後に投げたものの両方で照合する。** 古い応答を入れると、選んでいない
  // Project の結果を出す（`.claude/rules/async-ui.md`）。
  const fieldsRequest = useRef(0);

  /**
   * Project を選ぶ。**確定はしない。** 作成に要るフィールドを 1 度だけ確かめる。
   *
   * 引くのは押したときだけ。一覧を出しただけでは引かない（Project の数だけ
   * 問い合わせが増える、ADR 0088）。
   */
  const chooseProject = useCallback(async (project: Project) => {
    const request = ++fieldsRequest.current;

    setChosen(project);
    setFieldsCheck(null);
    setError(null);

    try {
      const check = await githubApi.projectFields(project.id);
      if (request !== fieldsRequest.current || check.projectId !== project.id) return;
      setFieldsCheck(check);
    } catch (e) {
      if (request !== fieldsRequest.current) return;
      // 問い合わせられなかったことは、確かめられなかったとして見せる。
      // **足りないにも揃っているにも倒さない。** 選ぶことも止めない。
      log.warn("作成に要るフィールドを確かめられませんでした", e);
      setFieldsCheck({ projectId: project.id, status: "unknown", missingFields: [] });
    }
  }, []);

  const confirmProject = useCallback(async () => {
    if (!repository || !chosen) return;

    setSaving(true);
    setError(null);
    try {
      await onSelected({
        repositoryOwner: repository.owner,
        repositoryName: repository.name,
        projectId: chosen.id,
        // 番号と名前と URL は表示用のスナップショット（ADR 0019 / 0025）。
        // この画面が見せていたものをそのまま送る。projectId は不透明な
        // node ID なので、送らないと一覧に「名称未取得」と出るしかなくなる。
        //
        // **URL も組み立てずに送る。** owner が user か org かで形が変わり、
        // フロントもサーバーもどちらなのかを知らない。GitHub が返したものを
        // ここまで運んできてある。
        projectNumber: chosen.number,
        projectTitle: chosen.title,
        projectUrl: chosen.url,
      });
    } catch (e) {
      setError(describeFailure("作成先を設定できませんでした", e));
    } finally {
      setSaving(false);
    }
  }, [chosen, onSelected, repository]);

  return (
    <div className="picker">
      <h1>{title}</h1>
      <p className="hint">{"このボードの draft issue を作る先を選びます。"}</p>
      <p className="hint">
        {"最初の draft issue を作ると、以後は変更できなくなります。"}
      </p>

      {error && <ErrorNotice failure={error} />}

      <section className="panel-section">
        <h2>リポジトリ</h2>
        {/*
          候補を取り切っていないことを出す（ADR 0054）。**絞り込みの手前に
          置く。** 打った語で出てこなかったときに、初めて読む場所ではなく
          最初から見えている必要がある。目当てが出ない理由が「権限が無い」
          「インストールしていない」「見た範囲の外」のどれなのかを、利用者が
          区別できるようにする（中核思想 3）。
        */}
        {truncated && (
          <p className="hint">
            {"候補が多いため、途中まで見て一覧を打ち切っています。"}
            {"目当てのリポジトリが出ないときは、この一覧の外にあるかもしれません。"}
          </p>
        )}
        {repositories !== null && repositories.length > 0 && (
          <p className="filter">
            <label htmlFor="repository-filter">名前で絞り込む</label>
            <input
              id="repository-filter"
              type="search"
              value={filter}
              placeholder="owner/name の一部"
              // 絞り込みは選び直しを伴わない。設定中でも触れてよい。
              onChange={(e) => setFilter(e.target.value)}
            />
          </p>
        )}
        {visible === null ? (
          <p className="hint">読み込み中…</p>
        ) : repositories !== null && repositories.length === 0 ? (
          // 権限不足と「本当に 1 つも無い」は API からは区別できない。
          // どちらの可能性も書いておく。**ここで止まる人はボードを作れない**
          // ので（ADR 0017）、行き止まりの理由が読める必要がある。
          //
          // **打ち切っているときは権限の話にしない。** 見ていない範囲が残って
          // いるのに「権限を確認してください」と言うと、確認しても何も出てこない
          // ところへ案内することになる（ADR 0054）。
          truncated ? (
            <p className="hint">
              {"見た範囲にはリポジトリがありませんでした。"}
              {"候補が多いため一覧を途中で打ち切っているので、"}
              {"この先にある可能性があります。"}
            </p>
          ) : (
            <p className="hint">
              {"リポジトリが 1 つも見つかりませんでした。"}
              {"GitHub App を入れたリポジトリがあるか、"}
              {"PAT で動かしているなら repo の read 権限があるかを確認してください。"}
            </p>
          )
        ) : visible.length === 0 ? (
          // 絞り込んだ結果が 0 件。**「1 つも無い」と言い分ける。** あちらは
          // 権限の話で、こちらは打った語の話。同じ文にすると、直す場所を
          // 間違える。
          <p className="hint">
            {`「${filter}」に当てはまるリポジトリはありません。`}
            {"打った語を短くしてみてください。"}
          </p>
        ) : (
          <ul className="plain-list">
            {visible.map((r) => (
              <li key={`${r.owner}/${r.name}`}>
                <button
                  type="button"
                  // 選択中であることを class だけで表すと、色の違いを見ない
                  // 利用者には伝わらない。状態として持たせる。
                  aria-pressed={
                    repository?.owner === r.owner && repository?.name === r.name
                  }
                  className={
                    repository?.owner === r.owner && repository?.name === r.name
                      ? "active"
                      : ""
                  }
                  // 設定の最中に選び直させない。送っている中身は押した時点の
                  // ものなので取り違えはしないが、画面だけ先に進むと、どれで
                  // 確定したのか分からなくなる。
                  disabled={saving}
                  onClick={() => void chooseRepository(r)}
                >
                  {r.owner}/{r.name}
                  {r.description && <span className="kind">{r.description}</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {repository && (
        <section className="panel-section">
          <h2>
            {repository.owner}/{repository.name} のプロジェクト
          </h2>
          {projectsFailed ? null : projects === null ? (
            <p className="hint">読み込み中…</p>
          ) : projects.length === 0 ? (
            // 候補は書ける Project だけに絞っている（ADR 0014）。読めるだけの
            // Project は 0 件に数えられるので、「無い」とだけ言うと、作る必要の
            // 無いものを作りにいかせる。**無いのか書けないのかは API からは
            // 区別できない**（ADR 0017）ので、両方を言う（#293）。
            <p className="hint">
              {"このリポジトリには、書き込める Projects v2 が紐づいていません。"}
              {"紐づく Project が無いか、あっても書く権限がありません。"}
              {
                "GitHub 側で Project を作って紐づけるか、書く権限をもらってから選び直してください。"
              }
            </p>
          ) : (
            <ul className="plain-list">
              {projects.map((p) => (
                <li key={p.id}>
                  <button
                    type="button"
                    aria-pressed={chosen?.id === p.id}
                    className={chosen?.id === p.id ? "active" : ""}
                    disabled={saving}
                    onClick={() => void chooseProject(p)}
                  >
                    #{p.number} {p.title}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {chosen && (
        <section className="panel-section picker-confirm" aria-label="選んだ作成先">
          <h2>
            #{chosen.number} {chosen.title} を作成先にする
          </h2>
          {/*
            確かめた結果は状態として出す（失敗の帯にしない）。足りなくても
            選べるので、選ぶ前の注意書きとして読ませる（ADR 0088）。
          */}
          <p
            className={fieldsCheck?.status === "missing" ? "hint fields-missing" : "hint"}
            role="status"
          >
            {projectFieldsNotice(fieldsCheck)}
          </p>
          <button
            type="button"
            className="primary"
            // 確かめた結果では止めない。止めるのは設定の最中だけ。
            disabled={saving}
            onClick={() => void confirmProject()}
          >
            この Project にする
          </button>
        </section>
      )}

      {onCancel && (
        <button type="button" className="quiet" onClick={onCancel}>
          やめる
        </button>
      )}
    </div>
  );
}
