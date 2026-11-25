# Omarchy theme backgrounds

The built-in Omarchy source follows the active native theme. Panora reads the
theme name and palette from the Omarchy state under
`~/.local/state/omarchy/current/`. It looks for stills directly inside the active
theme's `backgrounds` directory and under `~/.config/omarchy/backgrounds/<theme>/`.
It recognizes regular JPEG, PNG, and static WebP files, including native file
links. Panora leaves nested introductions, animated WebP, videos and invalid
images with Omarchy. The native desktop can continue to play its intros and
videos.

Discovery checks actual decoded dimensions before reporting stills in the gallery.
Playback revalidates selected bytes using the shared cache and image rules.
Discovery examines at most 512 candidate stills per active theme. A read failure
or an empty collection appears in the source editor. With a sole enabled source
and no eligible stills, the current wallpaper stays in place.

Discovery prepares bounded thumbnails. The editor shows four by default and up
to twelve per gallery page. The gallery serves prepared data and follows the
existing read-only preview policy. Exclude a wallpaper from its **Now** or
**History** actions.

The [configuration reference](../configuration.md#omarchy-theme-source)
describes the singleton source and per-source image rules.
The [desktop reference](../desktop.md#omarchy) covers rotation handoff.
