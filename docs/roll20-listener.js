window.Jukebox.scanForNewPlays = function() {
    tmp.apply(this, arguments)
    // If user is the GM of the current game, we then send the jukebox state
    if(window.is_gm){
        sendJukeboxState()
            .then( () => console.log("Sent Jk state"))
            .catch(err => console.log(`error sending jk state ${err}`))
    }
}
let srcMap = new Map()
async function sendJukeboxState(){
    let models = Jukebox.playlist.map(async (p) => {
        const attr = p.attributes
        return {
            title: attr.title,
            url: await getSrc(p),
            loop : attr.loop,
            playing : attr.playing,
            volume : attr.volume,
            progress: attr.gprogress,
            duration: p.prettyDuration,
        }
    });
    let m = await Promise.all(models)
    let filtered = m.filter(p => p.url != "")
    let payload =  {
        uId : String(window.d20_player_id),
        tracks: filtered,
        rId: String(window.campaign_id),
        date : new Date().toJSON()
    }
    console.log(payload)
    $.post('<JKBSYNC_URL>')
        .done( (msg) => console.log(msg))
        .fail( (xhr, textStatus, errorThrown) => console.log(`Error while sending jk state : ${errorThrown}`))

    async function getSrc(p) {
        const id = p.get('track_id')
        if(srcMap.has(id)) {
            return srcMap.get(id)
        }
        let url = ""
        switch(p.get('source')){
            case "Tabletop Audio":
                url = `https://s3.amazonaws.com/cdn.roll20.net/ttaudio/${ id.split('-')[0] }`
                break;
            case "Incompetech":
                url = `https://s3.amazonaws.com/cdn.roll20.net/incompetech/${ id.split('-')[0] }`
                break;
            case "My Audio":
                try {
                    url =  (await fetch(`https://app.roll20.net/audio_library/play/${campaign_id}/${id.split('-')[0]}`)).url
                }catch(e){
                    console.log("jk state : Could not get url for track p :")
                    console.log(p)
                }
                break;
            case "Battlebards":
                // Ignore if not playing, jquery is too costly
                let W = id.split('.mp3-') [0];
                W += '.mp3';
                W = W.replace(/%20%2D%20/g, ' - ');
                W = encodeURIComponent(W);
                const urlProm = new Promise( (res, rej) => {
                    $.post('/editor/audiourl/bb', {
                        trackurl: W
                    }, X => res(X))
                })
                const timeout = new Promise((res) => setTimeout(() => res("p1"), 2000));
                url = await Promise.race([urlProm, timeout])
                break
            default:
                console.log(`Jukebox : Omitting this :`)
                console.log(p)
        }
        srcMap.set(id, url)
        return url
    }
}