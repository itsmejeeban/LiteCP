# File manager upload behavior

The file manager has no application-imposed per-file size or queue-count cap. The browser uploads selected files sequentially, one request per file, with per-file progress. The dedicated Nginx upload location has `client_max_body_size 0` and request buffering disabled. Other API/archive limits remain unchanged.

Real limits still include available disk space, multipart staging space, filesystem limits, browser/proxy limits and connection timeouts. The panel retains its existing 45-minute per-request server timeout; an individual file must complete within the available connection window. For very large or long-running transfers, use the website FTPS account.

Multi-file selections reject existing-name conflicts and never overwrite those destinations. A single selected file retains the existing replacement behavior. Duplicate names in one queue are rejected. Each successful queued file is committed independently; later failures do not remove earlier completed uploads. Refresh the directory before retrying a partially completed queue.

Website path confinement, symlink protection and atomic managed writes remain enforced. The text editor size limit and archive expansion limits are unchanged. The large explanatory notice is removed from the file-manager page; the toolbar, address, filter, selection and footer spacing are compacted, and the duplicate breadcrumb row is hidden to prioritize the file list.

Validation covers the unbounded reader beyond 512 MiB, preservation of bounded-writer behavior, relevant file-security tests, JavaScript syntax, and Nginx configuration. It does not prove an actual multi-gigabyte browser transfer. Browser visual validation was unavailable during this update.
