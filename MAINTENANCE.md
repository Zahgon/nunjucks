
# Pushing a New Version

Nunjucks attempts to adhere to semantic versioning. The API is very stable, so
from here on out it will most likely be point releases.

1. Do a `pull` from github to make sure you have all the latest updates.

2. View all the changes since the last version:

   ```
   $ git log --oneline v1.2.3..master
   ```

Replace `v1.2.3` with whatever the last version was, and you'll see all the
changes going out in this version. Ensure that all significant user-facing
changes (new features and bugfixes) are mentioned in `CHANGELOG.md`. Change the
"master (unreleased)" heading in `CHANGELOG.md` to the new version number and
date.

3. Run `go vet ./...` and `go test ./...`.

4. Commit above changes and push to `master` (or a release branch, if using one).

6. Draft a new release on GitHub and copy the changelog to the description. The
   tag and title should both be the version, in the form `v2.3.0`. Publish the
   release.

7. The tag is the release: Go users fetch the module at that version
   (`go install github.com/mozilla/nunjucks/cmd/nunjucks-render@v2.3.0`).

8. Make sure docs are up-to-date. ``make prod`` in the ``docs/`` dir builds the
   site. Push (force push if necessary) the build out _site folder onto the
   `gh-pages` branch of the `nunjucks` repo to get it live. One way to do that
   is the following commands. These commands presume that you have another
   nunjucks git clone inside the (git-ignored) `docs/_site` directory, checked
   out to the `gh-pages` branch (and tracking `origin/gh-pages`). (To set that
   up the first time, `cd docs/_site`, `rm -rf *`, `git clone
   git@github.com:mozilla/nunjucks.git .`, and `git checkout gh-pages`).

   ```
   cd docs && make prod
   cd _site && git add -A && git commit && git push
   ```

9. Add a new "master (unreleased)" section at the top of `CHANGELOG.md`.
