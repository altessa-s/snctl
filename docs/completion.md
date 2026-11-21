# Shell completion

`snctl completion bash|zsh|fish|powershell` writes the shell's completion script.
Load it using the shell's normal completion mechanism, for example:

```sh
source <(snctl completion zsh)
```

Cloud paths complete folder by folder from the selected profile's last cached
listing alone. Tab sends no request, reads no credentials, creates no cache
files and changes no recency. Missing or damaged listings produce no remote
candidates. `--no-cache` also suppresses them. Local notebook filename completion
continues to work.

Run `snctl ls` or `snctl sync` online to refresh the listing. A new tablet file
will complete after that refresh. Folder-only arguments omit files. Paths keep
the exact canonical tablet spelling (`NOTE/Note`, `DOCUMENT/Document`). This
CLI resolves paths rather than identifier prefixes, so completion returns paths.

`highlights` completes PDF/EPUB documents that have an exact full-name `.mark`
sibling, as well as raw `.mark` files and folders. Local checks need no login;
remote candidates come from the existing cached listing, with no body downloads.
