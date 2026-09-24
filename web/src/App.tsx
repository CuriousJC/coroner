import { memo, useDeferredValue, useEffect, useMemo, useState } from "react";
import type { Entry, Length, Timeline } from "./types";

// How much of a long entry shows before it is expanded, and the length past
// which an entry is collapsed at all. Matches the static page.
const PREVIEW = 500;
const LONG = 1000;
const BADGES = 6;

// Labels for the length buckets, shortest first. Matches the static page.
const LENGTHS: [Length, string][] = [
  ["short", "under 250 words"],
  ["medium", "250 to 999"],
  ["long", "1,000 or more"],
];

type Order = "newest" | "oldest";

interface Month {
  key: string;
  label: string;
  entries: Entry[];
}

interface Year {
  year: string;
  count: number;
  months: Month[];
}

export function App() {
  const [data, setData] = useState<Timeline | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const [hiddenLengths, setHiddenLengths] = useState<ReadonlySet<Length>>(new Set());
  const [hideQuotes, setHideQuotes] = useState(false);
  const [order, setOrder] = useState<Order>("newest");
  const deferredQuery = useDeferredValue(query);

  useEffect(() => {
    fetch("api/writing.json")
      .then((r) => {
        if (!r.ok) throw new Error(`${r.status} ${r.statusText}`);
        return r.json() as Promise<Timeline>;
      })
      .then(setData)
      .catch((e: unknown) => setError(String(e)));
  }, []);

  // Lowercased once, so filtering is a substring scan rather than a lowercase
  // of the whole corpus on every keystroke.
  const haystack = useMemo(
    () => data?.entries.map((e) => `${e.title ?? ""}\n${e.text}`.toLowerCase()) ?? [],
    [data],
  );

  const visible = useMemo(() => {
    if (!data) return [];
    const q = deferredQuery.trim().toLowerCase();
    return data.entries.filter(
      (e, i) =>
        !hidden.has(e.source) &&
        !hiddenLengths.has(e.length) &&
        !(hideQuotes && e.quote) &&
        (!q || haystack[i].includes(q)),
    );
  }, [data, haystack, hidden, hiddenLengths, hideQuotes, deferredQuery]);

  const counts = useMemo(() => {
    const lengths: Record<string, number> = {};
    let quotes = 0;
    for (const e of data?.entries ?? []) {
      lengths[e.length] = (lengths[e.length] ?? 0) + 1;
      if (e.quote) quotes++;
    }
    return { lengths, quotes };
  }, [data]);

  const { undated, years } = useMemo(() => group(visible, order), [visible, order]);

  const badge = useMemo(() => {
    const out: Record<string, number> = {};
    data?.sources.forEach((s, i) => (out[s.name] = i % BADGES));
    return out;
  }, [data]);

  if (error) {
    return (
      <div className="wrap">
        <p className="notice">
          Could not load the corpus: {error}. This page is served by <code>coroner serve</code>, which needs to be running.
        </p>
      </div>
    );
  }
  if (!data) {
    return (
      <div className="wrap">
        <p className="notice">Loading…</p>
      </div>
    );
  }

  const dated = data.entries.filter((e) => e.published);
  const newest = dated.length ? day(dated[0].published!) : "";
  const oldest = dated.length ? day(dated[dated.length - 1].published!) : "";

  const toggle = (name: string) => setHidden((prev) => flip(prev, name));
  const toggleLength = (l: Length) => setHiddenLengths((prev) => flip(prev, l));

  const undatedSection =
    undated.length > 0 ? (
      <section className="year" id="undated">
        <h2>Undated</h2>
        <section className="month">
          {undated.map((e) => (
            <EntryCard key={e.id} e={e} badge={badge[e.source] ?? 0} />
          ))}
        </section>
      </section>
    ) : null;

  return (
    <div className="wrap" id="top">
      <header className="top">
        <h1>Everything written</h1>
        <p className="summary">
          {data.entries.length.toLocaleString()} pieces of writing
          {oldest && `, ${oldest} to ${newest}`}.{" "}
          {order === "newest" ? "Newest first: the oldest is at the bottom." : "Oldest first: the newest is at the bottom."}
          {data.folded > 0 && ` ${data.folded.toLocaleString()} more are copies of writing listed once, under its best copy.`}
        </p>

        <div className="controls">
          <input
            type="search"
            placeholder="Filter by words in the text or title"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Filter"
          />
          <div className="order" role="group" aria-label="Order">
            <button aria-pressed={order === "newest"} onClick={() => setOrder("newest")}>
              Newest first
            </button>
            <button aria-pressed={order === "oldest"} onClick={() => setOrder("oldest")}>
              Oldest first
            </button>
          </div>
        </div>

        <div className="sources">
          {data.sources.map((s) => (
            <label key={s.name}>
              <input type="checkbox" checked={!hidden.has(s.name)} onChange={() => toggle(s.name)} />{" "}
              <span className={`badge b${badge[s.name]}`}>{s.name}</span>{" "}
              <span className="n">{s.entries.toLocaleString()}</span>
            </label>
          ))}
        </div>

        <div className="sources">
          {LENGTHS.map(([l, label]) => (
            <label key={l}>
              <input type="checkbox" checked={!hiddenLengths.has(l)} onChange={() => toggleLength(l)} /> {label}{" "}
              <span className="n">{(counts.lengths[l] ?? 0).toLocaleString()}</span>
            </label>
          ))}
          {counts.quotes > 0 && (
            <label>
              <input type="checkbox" checked={!hideQuotes} onChange={() => setHideQuotes(!hideQuotes)} /> quotes{" "}
              <span className="n">{counts.quotes.toLocaleString()}</span>
            </label>
          )}
        </div>

        <nav className="years">
          {order === "newest" && undated.length > 0 && <a href="#undated">undated</a>}
          {years.map((y) => (
            <a key={y.year} href={`#y${y.year}`}>
              {y.year} <span>{y.count.toLocaleString()}</span>
            </a>
          ))}
          {order === "oldest" && undated.length > 0 && <a href="#undated">undated</a>}
        </nav>

        {visible.length !== data.entries.length && (
          <p className="showing">
            Showing {visible.length.toLocaleString()} of {data.entries.length.toLocaleString()}
          </p>
        )}
      </header>

      <main>
        {order === "newest" && undatedSection}
        {years.map((y) => (
          <section className="year" id={`y${y.year}`} key={y.year}>
            <h2>{y.year}</h2>
            {y.months.map((m) => (
              <section className="month" key={m.key}>
                <h3>{m.label}</h3>
                {m.entries.map((e) => (
                  <EntryCard key={e.id} e={e} badge={badge[e.source] ?? 0} />
                ))}
              </section>
            ))}
          </section>
        ))}
        {order === "oldest" && undatedSection}
        {visible.length === 0 && <p className="notice">Nothing matches.</p>}
      </main>

      <footer id="end">
        <a href="#top">Back to the top</a>
      </footer>

      <nav className="jump" aria-label="Jump">
        <button onClick={() => window.scrollTo({ top: 0 })} title="Top">
          ↑
        </button>
        <button onClick={() => window.scrollTo({ top: document.body.scrollHeight })} title="Bottom">
          ↓
        </button>
      </nav>
    </div>
  );
}

const EntryCard = memo(function EntryCard({ e, badge }: { e: Entry; badge: number }) {
  const long = e.text.length > LONG;
  const [open, setOpen] = useState(false);

  return (
    <article className="entry" id={`d-${e.id}`}>
      <div className="meta">
        {e.published && <time dateTime={day(e.published)}>{day(e.published)}</time>}
        <span className={`badge b${badge}`}>{e.source}</span>
        <span>{e.words.toLocaleString()} words</span>
      </div>
      {e.title && (
        <h4>
          {e.url ? (
            <a href={e.url} rel="noreferrer noopener" target="_blank">
              {e.title}
            </a>
          ) : (
            e.title
          )}
        </h4>
      )}
      <div className="text">{long && !open ? preview(e.text) : e.text}</div>
      {long && (
        <button className="more" onClick={() => setOpen(!open)}>
          {open ? "Collapse" : `Read all ${e.words.toLocaleString()} words`}
        </button>
      )}
      {e.copies && e.copies.length > 0 && (
        <p className="copies">
          Also in{" "}
          {e.copies.map((c) => c.source + (c.published ? ` (${day(c.published)})` : "")).join(", ")}
        </p>
      )}
    </article>
  );
});

// group splits entries into undated and year/month sections. The API sends
// entries newest first with undated last; oldest-first reverses the dated part.
function group(entries: Entry[], order: Order): { undated: Entry[]; years: Year[] } {
  const undated = entries.filter((e) => !e.published);
  const dated = entries.filter((e) => e.published);
  if (order === "oldest") dated.reverse();

  const years: Year[] = [];
  for (const e of dated) {
    const d = day(e.published!);
    const y = d.slice(0, 4);
    const mk = d.slice(0, 7);

    let year = years[years.length - 1];
    if (!year || year.year !== y) {
      year = { year: y, count: 0, months: [] };
      years.push(year);
    }
    let month = year.months[year.months.length - 1];
    if (!month || month.key !== mk) {
      month = { key: mk, label: monthLabel(mk), entries: [] };
      year.months.push(month);
    }
    month.entries.push(e);
    year.count++;
  }
  return { undated, years };
}

function flip<T>(set: ReadonlySet<T>, item: T): ReadonlySet<T> {
  const next = new Set(set);
  if (next.has(item)) next.delete(item);
  else next.add(item);
  return next;
}

// Dates arrive in UTC and are shown as UTC days, the same as every other
// coroner output.
function day(published: string): string {
  return published.slice(0, 10);
}

function monthLabel(key: string): string {
  const [y, m] = key.split("-").map(Number);
  const name = new Date(Date.UTC(y, m - 1, 1)).toLocaleString("en", { month: "long", timeZone: "UTC" });
  return `${name} ${y}`;
}

function preview(text: string): string {
  const flat = text.split(/\s+/).join(" ");
  if (flat.length <= PREVIEW) return flat;
  let head = flat.slice(0, PREVIEW);
  const space = head.lastIndexOf(" ");
  if (space > PREVIEW / 2) head = head.slice(0, space);
  return head.replace(/[\s,.;:-]+$/, "") + "…";
}
