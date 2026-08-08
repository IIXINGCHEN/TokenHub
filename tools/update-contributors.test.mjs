import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  fetchContributors,
  renderContributors,
  replaceContributorSection,
} from "./update-contributors.mjs";

const contributors = [
  {
    login: "alice",
    avatar_url: "https://avatars.example/alice",
    html_url: "https://github.com/alice",
  },
  {
    login: "release[bot]",
    avatar_url: "https://avatars.example/bot",
    html_url: "https://github.com/apps/release",
  },
];

describe("contributor rendering", () => {
  it("renders GitHub users and excludes bots", () => {
    const rendered = renderContributors(contributors);

    assert.match(rendered, /href="https:\/\/github\.com\/alice"/);
    assert.match(rendered, /<b>alice<\/b>/);
    assert.doesNotMatch(rendered, /release\[bot\]/);
  });

  it("escapes contributor-controlled values", () => {
    const rendered = renderContributors([
      {
        login: 'a<&"',
        avatar_url: 'https://avatars.example/a?x=1&y="2"',
        html_url: "https://github.com/a?x=1&y=2",
      },
    ]);

    assert.match(rendered, /a&lt;&amp;&quot;/);
    assert.match(rendered, /x=1&amp;y=&quot;2&quot;/);
  });
});

describe("README replacement", () => {
  it("replaces only the marked contributor section", () => {
    const readme = [
      "before",
      "<!-- readme: contributors -start -->",
      "old",
      "<!-- readme: contributors -end -->",
      "after",
    ].join("\n");

    assert.equal(
      replaceContributorSection(readme, "new"),
      [
        "before",
        "<!-- readme: contributors -start -->",
        "",
        "new",
        "",
        "<!-- readme: contributors -end -->",
        "after",
      ].join("\n"),
    );
  });

  it("rejects missing or duplicate markers", () => {
    assert.throws(
      () => replaceContributorSection("README", "new"),
      /must contain/,
    );
    assert.throws(
      () =>
        replaceContributorSection(
          "<!-- readme: contributors -start -->\n<!-- readme: contributors -start -->\n<!-- readme: contributors -end -->",
          "new",
        ),
      /duplicate/,
    );
  });
});

describe("GitHub contributor pagination", () => {
  it("requests pages until GitHub returns fewer than 100 entries", async () => {
    const requests = [];
    const firstPage = Array.from({ length: 100 }, (_, index) => ({
      login: `user-${index}`,
    }));
    const fetchImpl = async (url, options) => {
      requests.push({ url, options });
      return {
        ok: true,
        async json() {
          return requests.length === 1 ? firstPage : [{ login: "last-user" }];
        },
      };
    };

    const result = await fetchContributors(
      "astaxie/TokenHub",
      "test-token",
      fetchImpl,
    );

    assert.equal(result.length, 101);
    assert.match(requests[0].url, /page=1$/);
    assert.match(requests[1].url, /page=2$/);
    assert.equal(
      requests[0].options.headers.Authorization,
      "Bearer test-token",
    );
  });

  it("rejects malformed repository names", async () => {
    await assert.rejects(
      () => fetchContributors("not-a-repository", "token"),
      /Invalid/,
    );
  });
});
