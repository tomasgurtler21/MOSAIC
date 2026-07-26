// Package deploy executes a domain.Plan, writing files to the resolved deployment root, managing
// backups for locally-modified files, performing hook registration steps, writing the manifest,
// and delegating todo collection. The executor resolves the deployment root location once before
// any content write (CD-12) and records every action taken as a domain.ActionRecord so the
// manifest reflects actual on-disk state even when a partial failure occurs.
package deploy
