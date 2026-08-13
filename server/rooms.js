const { customAlphabet } = require('nanoid');

// No 0/O/1/I to avoid ambiguity when players read the code off the TV.
const genCode = customAlphabet('ABCDEFGHJKLMNPQRSTUVWXYZ23456789', 4);

/** @typedef {{id:string,name:string,avatarId:string,socketId:string,connected:boolean}} Player */

class RoomStore {
  constructor() {
    /** @type {Map<string, any>} */
    this.rooms = new Map();
    /** @type {Map<string, string>} socketId -> roomCode */
    this.socketRoom = new Map();
  }

  createRoom(hostSocketId) {
    let code;
    do {
      code = genCode();
    } while (this.rooms.has(code));

    const room = {
      code,
      hostSocketId,
      players: new Map(),
      currentGame: null,
      gameState: null,
    };
    this.rooms.set(code, room);
    this.socketRoom.set(hostSocketId, code);
    return room;
  }

  getRoom(code) {
    return this.rooms.get(String(code || '').toUpperCase());
  }

  addPlayer(code, { id, name, avatarId, socketId }) {
    const room = this.getRoom(code);
    if (!room) return null;
    const player = { id, name, avatarId, socketId, connected: true };
    room.players.set(id, player);
    this.socketRoom.set(socketId, room.code);
    return room;
  }

  removeBySocket(socketId) {
    const code = this.socketRoom.get(socketId);
    if (!code) return null;
    const room = this.rooms.get(code);
    this.socketRoom.delete(socketId);
    if (!room) return null;

    if (room.hostSocketId === socketId) {
      this.rooms.delete(code);
      return { room, hostLeft: true };
    }

    for (const [pid, p] of room.players) {
      if (p.socketId === socketId) {
        room.players.delete(pid);
        return { room, hostLeft: false, playerId: pid };
      }
    }
    return { room, hostLeft: false };
  }

  publicPlayers(room) {
    return [...room.players.values()].map((p) => ({
      id: p.id,
      name: p.name,
      avatarId: p.avatarId,
      connected: p.connected,
    }));
  }
}

module.exports = { RoomStore };
