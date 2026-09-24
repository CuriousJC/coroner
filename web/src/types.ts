// The shape of /api/writing.json, as internal/timeline writes it.

export interface Source {
  name: string;
  type: string;
  priority: number;
  entries: number;
}

export interface Copy {
  id: string;
  source: string;
  published?: string;
  score: number;
}

export interface Entry {
  id: string;
  source: string;
  type: string;
  title?: string;
  url?: string;
  published?: string;
  words: number;
  text: string;
  length: Length;
  quote?: boolean;
  copies?: Copy[];
}

// timeline.LengthOf's buckets: under 250 words, 250 to 999, 1,000 or more.
export type Length = "short" | "medium" | "long";

export interface Timeline {
  sources: Source[];
  entries: Entry[];
  documents: number;
  folded: number;
}
