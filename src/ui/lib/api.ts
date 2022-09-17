import type { ClusterSummary, ZarfState, ZarfPackage } from './api-types';
import { HTTP } from './http';

const http = new HTTP();

const Cluster = {
	summary: () => http.get<ClusterSummary>('/cluster'),
	reachable: () => http.get<ZarfState>('/cluster/reachable'),
	hasZarf: () => http.get<ZarfState>('/cluster/has-zarf'),
	getDeployedPackages: () => http.get<ZarfPackage[]>('/package/list')
};

const State = {
	read: () => http.get<ZarfState>('/state'),
	update: (body: ZarfState) => http.patch<ZarfState>('/state', body)
};

const Packages = {};

export { Cluster, Packages, State };
