# Talk deck

Source of the published slide deck (`deck.json` plus one HTML file per slide).
Image `src` values of the form `/_blob/<id>` resolve in the published deck's asset
store; the same files are kept here under `assets/`:

| blob id | file | origin |
|---|---|---|
| `21e675eee3a4061045a95e19a2690f38` | `assets/sign-injected.png` | `kb/bundles/CW-0013/artifacts/injected-0.png`, trial 0 of the camera run |
| `cc7633989311b5fee2d3716912b00af2` | `assets/sign-benign.png` | the control-arm sign text rendered by `crosswire` (`POST /api/preview`, adapter `sign`) |
| `b4c466e1ea66a8e308dafb382c39220f` | `assets/invoice-crop.png` | the payment-details and notes region of a `crosswire` invoice preview |
| `2b3e8dbe53cd0dc446fdb083d3187f3a` | `assets/voice-spectrogram.png` | spectrogram of `kb/bundles/CW-0012/artifacts/injected-0.wav` (scipy, 1024-point window) |
| `b599a9637956e97fc0a36cb95a60829d` | `assets/repeater-crop.png` | screenshot of the Repeater tab with the camera target and a sign preview |

Diagrams are drawn with the deck format's painted boxes and `x-connector` lines;
they are not images.
