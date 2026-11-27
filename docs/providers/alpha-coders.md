# Alpha Coders

Alpha Coders (Wallpaper Abyss) is an opt-in source for family-friendly desktop
wallpapers. Under **Sources → Add source → Alpha Coders**, connect a personal
API key, select tags, choose image rules, then save the source. The personal
tier allows open-source projects and has an account-wide quota of 30 requests
per minute and 500 per day. [Alpha Coders API documentation](https://api.alphacoders.com/api/instructions)

## Search and check results

- Pick up to five approved tags with typeahead. Panora matches any selected tag
  and deduplicates wallpapers found under multiple tags. The totals in
  suggestions are the catalog counts before applying image filters.
- Leave tags empty to browse by image rules alone. Use **More options** to
  search one keyword instead, or switch **Newest** to **Highest rated**. An
  official tag match uses that tag. Other keywords search image descriptions.
- Inherit shared Image rules or set a minimum-resolution preset or custom
  dimensions. **Similar to my screen** uses one detected display and defaults
  to 5% tolerance. Set the percentage to 0 for the exact shape. Panora pushes
  the minimum dimensions, a single exact resolution, and the similar-screen
  range to Alpha Coders when available. Playback checks the downloaded image
  again and uses the configured Fit or Crop mode.
- **Check results** checks up to two pages per tag or search. The combined
  count is distinct across tags and the per-tag counts can overlap. A check
  tells you when only a partial result was available. **Show more previews**
  shows more of the thumbnails from the checked pages. It does not fetch more
  pages. The result counts describe metadata and can differ from the images
  ready to play.

Discovery normally refreshes every two hours and reads up to three pages per
tag or search. It uses the shared discovery scheduler and honors rate limits.
Panora resolves and downloads originals as needed for playback, using its
bounded local playback cache.

## Credentials and attribution

**Manage key** replaces or removes the key for all Alpha Coders searches in
the current profile. Removing it makes the saved searches unavailable for
rotation until you connect again. The background service stores the key in a
separate owner-only file alongside the active configuration, with the suffix
`.alpha-coders-key` on the configuration filename. It sends the key only to
the Alpha Coders API. Typed tag prefixes, search terms and filters also go to
the API. Panora keeps the wallpaper’s title and public page link so you can
see attribution on Alpha Coders.

The documented default image license covers private, personal device use.
Keep any copies for that use and see the provider’s guidance for public,
redistributed or commercial use. [Alpha Coders image-use guidance](https://alphacoders.com/site/using-content)
