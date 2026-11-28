import type { EditorManifest, EditorOption } from './providerManifest';

const options = (pairs: [string, string][]): EditorOption[] =>
  pairs.map(([value, label]) => ({ value, label }));
const colors = [
  '#0066cc',
  '#66cccc',
  '#424153',
  '#333399',
  '#669900',
  '#ff9900',
  '#cc6633',
  '#ffffff',
  '#000000',
];

// Wallhaven's inline layout hosts the same fields as installed provider editors.
export const wallhavenManifest: EditorManifest = {
  version: 1,
  groups: [
    {
      id: 'query',
      fields: [
        {
          key: 'query',
          label: 'Search terms',
          control: 'text',
          default: '',
          placeholder: 'Mountains, forests, night skies…',
        },
      ],
    },
    {
      id: 'tags',
      fields: [
        {
          key: 'tags',
          label: 'Tags',
          control: 'multivalue',
          default: [],
          placeholder: 'Enter a tag…',
          suggest: 'suggest',
          help: 'Choose a suggestion or type a tag name and press Enter to add it.',
        },
      ],
    },
    {
      id: 'categories',
      fields: [
        {
          key: 'categories',
          label: 'Image categories',
          control: 'bitmask',
          default: '000',
          options: ['General', 'Anime', 'People'].map((label) => ({
            value: label.toLowerCase(),
            label,
          })),
          help: 'With none selected, search all safe-for-work categories.',
        },
      ],
    },
    {
      id: 'colors',
      fields: [
        {
          key: 'colors',
          label: 'Colors',
          control: 'swatches',
          default: [],
          options: colors.map((value) => ({ value, label: `Color ${value}` })),
          help: 'Match any selected color. Panora uses the closest Wallhaven swatch for each custom color.',
        },
      ],
    },
    {
      id: 'sorting',
      fields: [
        {
          key: 'sorting',
          label: 'Sort',
          control: 'select',
          default: 'relevance',
          options: options([
            ['relevance', 'Relevance'],
            ['date_added', 'Date added'],
            ['random', 'Random'],
            ['views', 'Views'],
            ['favorites', 'Favorites'],
            ['toplist', 'Top list'],
          ]),
        },
      ],
    },
    {
      id: 'advanced',
      fields: [
        {
          key: 'order',
          label: 'Order',
          control: 'select',
          default: 'desc',
          options: options([
            ['desc', 'Descending'],
            ['asc', 'Ascending'],
          ]),
        },
        {
          key: 'top_range',
          label: 'Top-list period',
          control: 'select',
          default: '1M',
          visible_when: { key: 'sorting', equals: 'toplist' },
          options: options([
            ['1d', '1 day'],
            ['3d', '3 days'],
            ['1w', '1 week'],
            ['1M', '1 month'],
            ['3M', '3 months'],
            ['6M', '6 months'],
            ['1y', '1 year'],
          ]),
        },
        {
          key: 'max_pages',
          label: 'Pages per refresh',
          control: 'integer',
          default: 20,
          min: 1,
          max: 100,
          required: true,
        },
      ],
    },
  ],
};

export function wallhavenGroup(id: string) {
  return wallhavenManifest.groups.find((group) => group.id === id)!;
}

export function wallhavenField(id: string) {
  return wallhavenGroup(id).fields[0];
}
