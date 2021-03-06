import React, { useState, useEffect, useRef } from 'react';
import { Link, useLocation, useHistory } from 'react-router-dom';
import { Icon, useKey, setTitle, Fetch, flexChildrenClass } from './common';

interface SearchState {
	search: string;
	results?: SearchResults;
}

interface SearchResults {
	Search: string;
	Results: SearchResult[] | null;
}

interface SearchResult {
	Type: string;
	Name: string;
	ID: number;
}

export default function Search() {
	const search = new URLSearchParams(window.location.search);
	const q = search.get('q') || '';

	const [data, setData] = useState<SearchState>({ search: q });
	const location = useLocation();
	const history = useHistory();
	const inputRef = useRef<HTMLInputElement>(null);
	const enterPress = useKey('enter');

	useEffect(() => {
		setTitle(q);
		if (inputRef && inputRef.current) {
			inputRef.current.focus();
		}
		Fetch<SearchResults>('Search?term=' + encodeURIComponent(q), res => {
			if (res && res.Results) {
				res.Results.sort((a, b) => {
					if (a.Type === b.Type) {
						return a.Name.localeCompare(b.Name);
					}
					if (a.Type === 'ship') {
						return -1;
					}
					if (b.Type === 'ship') {
						return 1;
					}
					if (a.Type === 'group') {
						return -1;
					}
					if (b.Type === 'group') {
						return 1;
					}
					debugger; // should be unreachable
					return 0;
				});
			}
			setData({ search: q, results: res });
		});
	}, [location, q]);

	useEffect(() => {
		if (enterPress && data.results && data.results.Results) {
			const r = data.results.Results[0];
			history.push('/?' + r.Type + '=' + r.ID.toString());
		}
	}, [enterPress, data.results, history]);

	return (
		<div className="flex flex-column">
			<div className={flexChildrenClass}>
				search:{' '}
				<input
					ref={inputRef}
					type="text"
					value={q}
					onChange={ev => {
						const v = ev.target.value;
						if (v === undefined) {
							return;
						}
						history.replace('/search?q=' + encodeURIComponent(v));
					}}
				/>
			</div>
			{data.results && data.results.Results ? (
				<div className={flexChildrenClass}>
					{data.results.Results.map(v => (
						<div key={v.ID} className="ma2">
							<Link to={'/?' + v.Type + '=' + v.ID.toString()}>
								{v.Type !== 'group' ? <Icon id={v.ID} alt={v.Name} /> : null}
								{v.Name}
							</Link>{' '}
							({v.Type})
						</div>
					))}
				</div>
			) : null}
		</div>
	);
}
