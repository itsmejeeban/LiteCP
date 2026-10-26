# PHP extension catalogue

The PHP extensions page provides text search, a PHP-version filter, and filters for application baseline, optional packages, installed/loaded items and packages available to install.

The list combines actual loaded modules with extension packages from configured OS repositories for installed LiteCP PHP runtimes. Loaded modules and packages are shown separately because one package can provide several modules. Baseline package labels cover WordPress, WHMCS and common PHP support; they do not mean every application requires every labeled package. Optional packages are never automatically installed. Existing installer defaults remain unchanged.

Installed baseline packages and core/runtime modules are protected from removal in this catalogue. PHP 7.3 container modules remain image-managed. Native runtime extension packages can be installed individually with confirmation; removing optional packages uses the package manager's non-cascading removal command. PHP-FPM restarts for that version, affecting all sites that use it. Package actions verify package state; the loaded module inventory is refreshed separately. A package providing a disabled module may require configuration before it appears loaded.

Availability is restricted to configured repositories and installed PHP versions, not every third-party or source-only PHP extension. Repository metadata must already be available (DNF uses its cache); refresh host repository metadata if the catalogue is stale. Runtime, developer/debug, and embedded-server packages are excluded. Package names are validated against the exact runtime prefix and rechecked against the catalogue on mutation.

References: [WordPress server environment](https://make.wordpress.org/hosting/handbook/server-environment/) and [WHMCS 9.0 requirements](https://docs.whmcs.com/9-0/installation-guide/system-requirements/). Match requirements to the application's release and plugins; ionCube compatibility is PHP-version-specific.
