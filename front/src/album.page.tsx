import { useOffline } from "./hook/useOffline";
import { useParam } from "./hook/useParam";
import { usePlayback } from "./hook/usePlayback";
import { useTracks } from "./hook/useTracks";
import {
  albumArtist,
  albumArtworkURL,
  isChromeIncompatible,
  splitByAlbum,
  totalTimeInLocal,
} from "./library";
import { TrackList } from "./trackList";

export default function Album() {
  const p = useParam();

  const tracks = useTracks();
  const tracksByAlbum = splitByAlbum(tracks);

  const { addQueue, resume } = usePlayback();

  const album = tracksByAlbum.find((v) => v.id == p["id"]);
  if (!album) {
    return <div>Not Found</div>;
  }

  const onClickArtwork = () => {
    const ids = album.tracks.map((v) => v.PersistentID);
    addQueue(...ids);
    resume();
  };

  const { save } = useOffline();

  const onDownloadClick = () => {
    for (const tr of album.tracks) {
      save(tr.PersistentID);
    }
  };

  const altrs = album.tracks;
  altrs.sort((a, b) => {
    if (a.DiscNumber != b.DiscNumber) {
      return a.DiscNumber - b.DiscNumber;
    }
    return a.TrackNumber - b.TrackNumber;
  });

  return (
    <div className="min-w-[350px] max-w-screen-screen3 ml-auto mr-auto pl-16 pr-16">
      <div className="grid grid-cols-[30%,1fr] gap-[24px]">
        <div className="relative cursor-pointer group" onClick={onClickArtwork}>
          <img
            crossOrigin="use-credentials"
            src={albumArtworkURL(tracksByAlbum, album.id)}
            className="aspect-square object-cover w-full"
          />
          <div className="absolute top-0 left-0 w-full h-full z-10 bg-transparent-press hidden group-hover:block">
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2">
              <pixiv-icon name="24/Play" scale="3" />
            </div>
          </div>
        </div>
        <div>
          <p className="text-lg font-semibold">{album.album}</p>
          <p>{albumArtist(album.tracks[0])}</p>
          <button onClick={onDownloadClick}>
            <pixiv-icon name="24/DownloadAlt" scale="2" />
          </button>
        </div>
      </div>
      <div className="mt-16 pr-8 pl-8 w-full">
        <TrackList tracks={altrs} />
      </div>
    </div>
  );
}
