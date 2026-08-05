package ignore

// Noise is the built-in exclude list: the directories and files an export drops
// alongside the writing that coroner has no use for.
//
// Kept short and uncontroversial on purpose. Anything source-specific -- the
// dozen top-level folders a Facebook export ships that are not posts -- belongs
// in that source's parser, which knows what it is looking at, rather than here
// where it would silently apply to every corpus.
var Noise = []string{
	// Version control and editor droppings, in case a corpus is kept in a repo
	// of its own.
	".git/",
	".svn/",
	".vscode/",
	".idea/",

	// Assets. Coroner indexes text; an export's media folder is usually the
	// overwhelming majority of its files and none of them parse.
	"*.jpg",
	"*.jpeg",
	"*.png",
	"*.gif",
	"*.webp",
	"*.mp4",
	"*.mov",
	"*.mp3",
	"*.m4a",
	"*.pdf",
	"*.zip",

	// Web export scaffolding.
	"*.css",
	"*.js",
	"*.woff",
	"*.woff2",
	"*.ttf",

	// OS artefacts.
	".DS_Store",
	"Thumbs.db",
	"desktop.ini",
}
