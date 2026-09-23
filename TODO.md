# TODO

Work not yet done. Remove an item when it is done.

## Search

- **Show the document ID in human output.** `search -format=json` carries `id`;
  the table does not, so a result cannot be cited without re-running as JSON.
  Always show it: 16 characters on a line that already has corpus, date and
  score.
- **Filter by date range**, e.g. `-from=2019 -to=2020`. Documents already carry
  dates, so this is a filter over loaded documents.
- **`-explain`**: show which query terms drove a lexical hit. Lexical only; the
  vector half has no honest equivalent.

## Needs Justin

- **An evaluation set.** About 20 queries, each with the piece of writing it
  should find. Without one, neither the embedding model nor `search -depth` can
  be judged. Include short posts as well as essays: most of the corpus is short
  Facebook text, which embeds differently from paragraphs.
- **Wrong dates in the HTML site's own source** (optional; coroner uses the
  filename date and is unaffected). The in-page `<h2>` disagrees with the
  filename on these pages:

  | page | filename | `<h2>` |
  | --- | --- | --- |
  | `2016.11.11-why-vote-for-trump` | 2016-11-11 | 2016-10-25 |
  | `2020-05-22-100k-dead` | 2020-05-22 | 2020-02-27 |
  | `2020-10-13-electoral-college` | 2020-10-13 | 2020-10-15 |
  | `2020-10-15-why-vote-biden` | 2020-10-15 | 2020-10-25 |
  | `2021-05-24-captialism` | 2021-05-24 | 2021-05-23 |
  | `2023-04-14-book-sale` | 2023-04-14 | 2023-04-13 |
  | `2024-01-29-link-dump` | 2024-01-29 | 2014-01-29 |
  | `2024-04-24-people-dont-buy-books` | 2024-04-24 | 2024-04-23 |
  | `2024-07-28-link-dump` | 2024-07-28 | 2024-07-29 |
  | `2024-09-19-Evangelicals-For-Kamala` | 2024-09-19 | 2024-09-13 |
  | `2025.07.28-oppenheimer` | 2025-07-28 | 2016-10-25 |
  | `2025.08.15-newsome-ca-tx-gerrymander` | 2025-08-15 | 2016-10-25 |
  | `2025.08.16-what-is-government` | 2025-08-16 | 2016-10-25 |
  | `2025.08.18-8-years-after-charlottesville` | 2025-08-18 | 2016-10-25 |

  Five pages also have a literal `<h1>title</h1>` left from a template.
