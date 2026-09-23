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
  copies?: Copy[];
}

export interface Timeline {
  sources: Source[];
  entries: Entry[];
  documents: number;
  folded: number;
}
