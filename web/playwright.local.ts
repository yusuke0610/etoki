import base from "/home/user/etoki/web/playwright.config";

/**
 * リポジトリの設定に executablePath だけを足したもの。**コミットしない。**
 *
 * この実行環境が持つ Chromium のリビジョン（1194）と、`@playwright/test` が
 * 要求するリビジョン（1228）が食い違っている。devShell では
 * `playwright-driver.browsers` が揃えてくれるので、リポジトリ側の設定を
 * 変える理由は無い。
 */
export default {
  ...base,
  projects: [
    {
      name: "chromium",
      use: {
        ...base.projects![0]!.use,
        launchOptions: { executablePath: "/opt/pw-browsers/chromium-1194/chrome-linux/chrome" },
      },
    },
  ],
};
