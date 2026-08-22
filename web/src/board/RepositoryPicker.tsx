import { useCallback, useEffect, useRef, useState } from "react";

import { githubApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import type { BoardTarget, Project, Repository } from "../api/types";
import { ErrorNotice } from "../ErrorNotice";

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
 */
export function RepositoryPicker({ title, onSelected, onCancel }: Props) {
  const [repositories, setRepositories] = useState<Repository[] | null>(null);
  const [repository, setRepository] = useState<Repository | null>(null);
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [error, setError] = useState<Failure | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;

    void (async () => {
      try {
        const list = await githubApi.repositories();
        if (!cancelled) setRepositories(list);
      } catch (e) {
        if (!cancelled) setError(describeFailure("リポジトリを取得できませんでした", e));
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  // リポジトリを続けて押すと応答が前後しうる。番号を振って最後に投げたものだけ
  // 反映する。古い応答で上書きすると、選んでいないリポジトリのプロジェクトが
  // 並ぶ。作成先を取り違える経路なので塞いでおく。
  const projectsRequest = useRef(0);

  const chooseRepository = useCallback(async (repo: Repository) => {
    const request = ++projectsRequest.current;

    setRepository(repo);
    setProjects(null);
    setError(null);

    try {
      const list = await githubApi.projects(repo.owner, repo.name);
      if (request !== projectsRequest.current) return;
      setProjects(list);
    } catch (e) {
      if (request !== projectsRequest.current) return;
      setError(describeFailure("プロジェクトを取得できませんでした", e));
    }
  }, []);

  const chooseProject = useCallback(
    async (project: Project) => {
      if (!repository) return;

      setSaving(true);
      setError(null);
      try {
        await onSelected({
          repositoryOwner: repository.owner,
          repositoryName: repository.name,
          projectId: project.id,
          // 番号と名前と URL は表示用のスナップショット（ADR 0019 / 0025）。
          // この画面が見せていたものをそのまま送る。projectId は不透明な
          // node ID なので、送らないと一覧に「名称未取得」と出るしかなくなる。
          //
          // **URL も組み立てずに送る。** owner が user か org かで形が変わり、
          // フロントもサーバーもどちらなのかを知らない。GitHub が返したものを
          // ここまで運んできてある。
          projectNumber: project.number,
          projectTitle: project.title,
          projectUrl: project.url,
        });
      } catch (e) {
        setError(describeFailure("作成先を設定できませんでした", e));
      } finally {
        setSaving(false);
      }
    },
    [onSelected, repository],
  );

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
        {repositories === null ? (
          <p className="hint">読み込み中…</p>
        ) : repositories.length === 0 ? (
          // 権限不足と「本当に 1 つも無い」は API からは区別できない。
          // どちらの可能性も書いておく。**ここで止まる人はボードを作れない**
          // ので（ADR 0017）、行き止まりの理由が読める必要がある。
          <p className="hint">
            {"リポジトリが 1 つも見つかりませんでした。"}
            {"GitHub App を入れたリポジトリがあるか、"}
            {"PAT で動かしているなら repo の read 権限があるかを確認してください。"}
          </p>
        ) : (
          <ul className="plain-list">
            {repositories.map((r) => (
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
          {projects === null ? (
            <p className="hint">読み込み中…</p>
          ) : projects.length === 0 ? (
            <p className="hint">
              {"このリポジトリに紐づく Projects v2 がありません。"}
              {"GitHub 側で作ってから選び直してください。"}
            </p>
          ) : (
            <ul className="plain-list">
              {projects.map((p) => (
                <li key={p.id}>
                  <button
                    type="button"
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

      {onCancel && (
        <button type="button" onClick={onCancel}>
          やめる
        </button>
      )}
    </div>
  );
}
