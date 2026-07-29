import { useEffect, useState } from "react";

/** バックエンド疎通の 3 状態。 */
type Health = "checking" | "ok" | "unreachable";

/**
 * App は Phase 0 時点ではバックエンドへの疎通確認のみを表示する。
 * Excalidraw の埋め込みと注釈 UI は Phase 2 で載せる。
 */
export function App() {
  const health = useHealth();

  return (
    <main>
      <h1>etoki</h1>
      <p>ブレストの絵を解いて、GitHub の設計に落とす。</p>
      <p>
        backend: <output>{health}</output>
      </p>
    </main>
  );
}

/** useHealth は /healthz を 1 回だけ叩き、その結果を返す。 */
function useHealth(): Health {
  const [health, setHealth] = useState<Health>("checking");

  useEffect(() => {
    // アンマウント後の setState を避けるため、abort で購読を打ち切る。
    const controller = new AbortController();

    fetch("/healthz", { signal: controller.signal })
      .then((res) => setHealth(res.ok ? "ok" : "unreachable"))
      .catch(() => {
        if (!controller.signal.aborted) {
          setHealth("unreachable");
        }
      });

    return () => controller.abort();
  }, []);

  return health;
}
