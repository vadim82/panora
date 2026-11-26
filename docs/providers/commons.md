# Wikimedia Commons

Open **Sources → Add source → Wikimedia Commons**. Use **Search Commons** to
find images and **More options** for the **Featured landscapes** collection.
Commons needs no account or API key. Panora contacts Commons when you run a
search or enable a saved source.

## Search Commons

Enter keywords. Under **More options**, you can search by a Commons category
name without the `Category:` prefix, on its own or with the keywords.
Search returns JPEG and PNG files. Minimum width and height narrow the results.
An exact width and height pair takes precedence over the minimums.

Each result shows its dimensions, creator credit, license and file page.
Thumbnail previews appear when Commons supplies them. Review the file and its
reuse terms, then approve the results you want in rotation and add the source.
A saved source can contain up to 36 reviewed files. You can search several
pages before saving, and edit the source later to add or remove approvals.
Commons categories describe content. Review images before approving them.
[Commons content disclaimer](https://commons.wikimedia.org/wiki/Commons:General_disclaimer)

Panora uses file search filters to narrow results, then checks dimensions,
format, attribution and reuse terms against the file metadata. Search accepts
Creative Commons Attribution, Attribution-ShareAlike, CC0 and Public Domain
Mark links, plus public-domain files whose Commons metadata and file page
identify their status. Panora uses the requested attribution when supplied.
Files with missing credit, missing reuse terms or recorded restrictions are
skipped. [Commons file filters](https://www.mediawiki.org/wiki/Help:CirrusSearch#File_properties_search) ·
[Commons reuse guidance](https://commons.wikimedia.org/wiki/Commons:Reusing_content_outside_Wikimedia)

Approval records the file's page ID, upload revision and reuse terms. Rotation
checks the saved approvals. Discovery and new downloads skip files whose
revision or reuse terms have changed. When the file page is the source for a
public-domain claim, an approval also records the page revision. Run a new
search to review and approve an updated file.

## Featured landscapes

This collection contains ten reviewed JPEGs from
[Featured pictures of landscapes](https://commons.wikimedia.org/wiki/Category:Featured_pictures_of_landscapes).
Each entry pins its Commons page ID, upload revision, original SHA-256, and
reviewed attribution and license fields. The manifest stores this review data.
When adding a file, review the original, verify its revision and required
credit and pin the SHA-256 of the reviewed bytes.

The collection has 16:9 choices and three images compatible with a 2560 × 1080
display at the default 10-pixel tolerance. It has no matches at that tolerance
for 3440 × 1440 or 5120 × 1440.

## Image rules and playback

Use [Image rules](../configuration.md#image-rules-for-each-source) for
minimum size, exact resolution and aspect ratio during rotation.
**Same shape as my screen** uses the configured pixel tolerance and requires
supported display geometry. **Source status** shows discovery and retry state.
**Now** and **History** retain the creator credit, license and file-page link.

The provider fetches up to eight results per search page and checks approved
file metadata in batches of up to 12. Originals use the
[shared playback cache](../architecture.md#shared-playback-cache) and must
match the approved upload digest before they can enter rotation. Panora
identifies itself to Commons with a contact URL, respects HTTP retry hints
and MediaWiki lag responses, and strips the known Imageinfo tracking fields
before fetching from canonical Commons URLs. [Imageinfo API](https://www.mediawiki.org/wiki/API:Imageinfo)
