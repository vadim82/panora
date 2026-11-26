# Wikimedia Commons

Choose **Sources → Add source → Wikimedia Commons** and select **Featured
landscapes**. The editor explains the network access and offers the usual
[Image rules](../configuration.md#image-rules-for-each-source). Adding a source
with **Include in rotation** enabled starts discovery without an account or API
key. Disable, edit or remove the source from its row. **Source status** shows
the discovery check and retry state. **Now** and **History** show the creator
credit, license and the original file-page action.

The initial collection consists of ten reviewed photos from
[Featured pictures of landscapes](https://commons.wikimedia.org/wiki/Category:Featured_pictures_of_landscapes).
Each entry pins its Commons page ID, uploaded file revision, original SHA-256,
and reviewed attribution and license fields. Discovery checks those fields with
the [Imageinfo API](https://www.mediawiki.org/wiki/API:Imageinfo). Playback
checks the original against the pinned digest before adding it to the shared
cache. Commons may change its file catalog between requests. Unknown files and
changed revisions or reuse terms are skipped.

Panora applies minimum size, exact resolution and shape rules locally.
**Same shape as my screen** uses the configured pixel tolerance and a supported
display geometry. For example, the initial set has 16:9 choices and three
images compatible with a 2560 × 1080 display at the default 10-pixel tolerance.
The reviewed set has no matches at that tolerance for 3440 × 1440 or
5120 × 1440. The feature serves a fixed collection. Subject keywords, arbitrary
Commons categories and Commons-wide search are outside this collection.

Discovery fetches metadata in batches of at most 12 reviewed file IDs.
Originals are selected, downloaded and validated through the
[shared playback cache](../architecture.md#shared-playback-cache).
Panora identifies itself to Commons with a contact URL and respects HTTP
retry hints and MediaWiki lag responses. The provider strips the documented
integration's known Imageinfo tracking fields from canonical Commons original
URLs before passing them to the normal public HTTPS rules. It does not store
user credentials.

The collection manifest contains review data, not image bytes. When considering
a change, review the original's content and verify its file-page license and
required credit together with its published revision. Use the file's requested
attribution when present and pin the SHA-256 of the exact reviewed original.
[Commons reuse guidance](https://commons.wikimedia.org/wiki/Commons:Reusing_content_outside_Wikimedia)
describes the applicable conditions.
