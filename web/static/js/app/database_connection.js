// Connection strings are built from the selected server, with secrets fetched
// explicitly. Escape each URI component independently (including IPv6 hosts).
App.DatabaseConnection = {
    needsPassword(type) { return ['mysql','postgres','mongodb','redis'].includes(type); },
    uri(db, host, revealed = false) {
        const address = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
        const user = encodeURIComponent(db.username || '');
        const password = revealed ? encodeURIComponent(db.password || '') : '<password>';
        const name = encodeURIComponent(db.database_name || '');
        const endpoint = `${address}:${db.host_port}`;
        switch (db.type) {
            case 'mysql': return `mysql://${user}:${password}@${endpoint}/${name}`;
            case 'postgres': return `postgresql://${user}:${password}@${endpoint}/${name}?sslmode=prefer`;
            case 'mongodb': return `mongodb://${user}:${password}@${endpoint}/${name}?authSource=admin`;
            case 'redis': return `redis://:${password}@${endpoint}`;
            case 'sqlite': return `/var/lib/gopulse/databases/${db.id}/sqlite.db`;
            case 'pocketbase': case 'appwrite': return `http://${endpoint}`;
            default: return '';
        }
    }
};
